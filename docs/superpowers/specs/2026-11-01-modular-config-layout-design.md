# Modular Multi-File Configuration Layout

*   **Author:** jskswamy
*   **Status:** Proposed
*   **Date:** 2026-11-01

--------------------------------------------------------------------------------

## Overview

Aide currently stores all user configuration in a single monolithic `~/.config/aide/config.yaml` file: global preferences, agent definitions, custom capabilities, sandbox policies, guard definitions, provisioning sets (`plugins`, `mcp_servers`, `hooks`), and every profile definition (`profiles:`). Once a user configures three or four profiles alongside custom capabilities, gateways, and MCP servers, `config.yaml` grows to hundreds of lines with deep YAML indentation. Worse, keeping all profiles in a single file makes it impossible to version-control personal profiles in a public dotfiles repository while keeping work or client profiles private.

This design splits `~/.config/aide/` into focused, un-nested YAML files organized by concern (`config.yaml`, `sandbox.yaml`, `provisioning.yaml`, and one file per profile under `profiles/<name>.yaml`), while preserving full read backward compatibility with existing single-file configurations.

## Goals

*   **Modular, flat YAML files by concern:**
    *   `~/.config/aide/config.yaml`: Global settings only (`default_profile`, `agents`, `preferences`, `statusline`).
    *   `~/.config/aide/sandbox.yaml`: Security and isolation definitions (`capabilities`, `sandboxes`, `custom_guards`, `guard_types`, `never_allow`, `never_allow_env`, `scrub_env`).
    *   `~/.config/aide/provisioning.yaml`: Shared tooling declarations (`plugins`, `mcp_servers`, `hooks`).
    *   `~/.config/aide/profiles/<name>.yaml`: One flat YAML file per profile (decoded directly as a profile without a top-level `profiles: <name>:` wrapper).
*   **100% Read Backward Compatibility:** Existing monolithic `config.yaml` files (both `profiles:` / `contexts:` full format and flat minimal format) continue to load without error.
*   **No Silent Shadowing:** Defining the same profile or key in both `config.yaml` and a split file (`profiles/<name>.yaml`, `sandbox.yaml`, or `provisioning.yaml`) fails config loading with an explicit duplicate-definition error.
*   **Write-Only-Changed-Bytes in Split Layout:** `WriteConfig` serializes only to the split file layout and compares serialized bytes against existing on-disk files, writing only files whose bytes actually changed (and removing `profiles/<deleted>.yaml` when a profile is removed).
*   **Migration via `aide config migrate` & Non-Intrusive Nudging:**
    *   `aide config migrate` upgrades legacy `contexts:` schemas (if present) AND splits monolithic `config.yaml` files into the new layout (keeping `config.yaml.bak`), while `aide profile migrate` remains available for in-place `contexts:` $\rightarrow$ `profiles:` schema/login migration.
    *   When `Load()` detects a legacy monolithic `config.yaml`, normal interactive runs display a brief migration nudge at most once every 7 days (tracked in `~/.local/state/aide/migrate-nudge.json`).
    *   Mutating CLI commands (`aide cap ...`, `aide profile ...`, `aide gateway ...`, `aide hook ...`, `aide sandbox ...`, `aide adopt`) prompt interactively to migrate first; if the user declines or runs non-interactively, `WriteConfig` refuses to overwrite a legacy monolithic config and directs the user to `aide config migrate --write`.

## Non-Goals

*   Switching from YAML to TOML (YAML remains the sole configuration language for parity with `sops` and `skillshare`).
*   Changing `.aide.yaml` project-level override files or `secrets/*.enc.yaml` files.
*   Supporting arbitrary user-named files in a free-form `conf.d/` directory.
*   Maintaining dual-write support for writing mutations back into a legacy monolithic `config.yaml`.

--------------------------------------------------------------------------------

## Approaches Considered

### Approach 1: Unified In-Memory `Config` with Scatter/Gather in `Load` and `WriteConfig` (Selected)

*   **How it works:**
    *   `config.Load(configDir, projectDir)` gathers `config.yaml`, `sandbox.yaml`, `provisioning.yaml`, and `profiles/*.yaml` into the existing in-memory `config.Config` struct.
    *   `WriteConfig` and `WriteConfigTo` scatter the in-memory `Config` struct into the 4 logical file targets (`config.yaml`, `sandbox.yaml`, `provisioning.yaml`, `profiles/<name>.yaml`), writing only files whose marshaled bytes differ from disk.
*   **Trade-offs:**
    *   *Pros:* Zero churn across the 30+ CLI command mutation sites in `cmd/aide/`. Every command continues to mutate `cfg` and call `config.WriteConfig(cfg)`.
    *   *Cons:* `WriteConfig` coordinates multiple atomic file writes and stale profile file cleanup.

### Approach 2: Per-Subsystem Loaders and Writers Across CLI Commands

*   **How it works:** Replace `config.WriteConfig(cfg)` with targeted writers (`WriteProfile(dir, name, ctx)`, `WriteSandbox(dir, sbx)`, `WriteProvisioning(dir, prov)`) at every CLI call site.
*   **Trade-offs:**
    *   *Pros:* Each CLI command explicitly states which file it mutates.
    *   *Cons:* Touches 30+ call sites across 10 files in `cmd/aide/`, complicates cross-cutting mutations (such as `aide profile delete` which updates both `profiles/<name>.yaml` and `default_profile` in `config.yaml`), and adds no disk I/O benefit once Approach 1 skips unchanged files via byte comparison.

--------------------------------------------------------------------------------

## Technical Specification

### 1. File Layout & Schemas

All files live under `configDir` (`$XDG_CONFIG_HOME/aide/` or `~/.config/aide/`):

```text
~/.config/aide/
├── config.yaml          # Global settings (default_profile, agents, preferences, statusline)
├── sandbox.yaml         # Optional: capabilities, sandboxes, custom_guards, guard_types, never_allow, never_allow_env, scrub_env
├── provisioning.yaml    # Optional: plugins, mcp_servers, hooks
├── profiles/            # Optional directory: one <name>.yaml per profile
│   ├── work.yaml
│   └── personal.yaml
└── secrets/             # Unchanged: sops/age encrypted secret stores
```

#### 1.1 `config.yaml` (Global Core)
Contains only:
```yaml
default_profile: personal
agents:
  claude:
    binary: claude
    icon: "🤖"
preferences:
  show_info: true
statusline:
  modules: [sandbox, network, capabilities]
```

#### 1.2 `sandbox.yaml` (Security & Capabilities)
Unmarshals into a dedicated `SandboxFile` struct with strict known-field checking (rejecting misplaced profile or provisioning keys):
```yaml
never_allow:
  - ~/.kube/prod-config
never_allow_env:
  - PRODUCTION_DB_PASSWORD
scrub_env:
  - CORP_LLM_*
capabilities:
  k8s-dev:
    extends: k8s
    deny: ["~/.kube/prod-config"]
sandboxes: {}
custom_guards: {}
guard_types: {}
```

#### 1.3 `provisioning.yaml` (Shared Tooling)
Unmarshals into a dedicated `ProvisioningFile` struct:
```yaml
plugins:
  jskswamy/claude-plugins: [commit-tools, craft]
mcp_servers:
  github:
    url: "https://api.githubcopilot.com/mcp"
hooks:
  pre_tool:
    - command: "rtk hook {agent}"
      matcher: shell
```

#### 1.4 `profiles/<name>.yaml` (Per-Profile Flat File)
*   Only files matching `*.yaml` (excluding `*.bak*` and hidden dotfiles `.*`) in `profiles/` are loaded.
*   The profile name is `strings.TrimSuffix(filename, ".yaml")` and must satisfy a valid profile identifier check (non-empty, no path separators).
*   The file contents unmarshal directly as a profile entry using the `profiles:` key vocabulary (`config_dir_name`, `config_dir`, `config_dirs`, `match`, `agent`, `secret`, `credentials`, `default_credentials`, `gateways`, `sandbox`, `capabilities`, `plugins`, `mcp_servers`, `hooks`, `yolo`, `env`, `icon`). Legacy `contexts:` keys (`profile:`, `profile_dir:`) inside `profiles/<name>.yaml` are rejected with the same error message as `decodeProfiles`.

### 2. Loading & Duplicate Detection (`config.Load`)

1.  **Read `config.yaml`:**
    *   If `config.yaml` does not exist and none of `sandbox.yaml`, `provisioning.yaml`, or `profiles/*.yaml` exist, return an empty `&Config{}, nil`.
    *   If `config.yaml` does not exist but split files *do* exist, initialize an empty `&Config{ProfileSchema: true}` and proceed to merge the split files.
2.  **Detect `LegacyMonolithic` State:**
    *   `cfg.LegacyMonolithic` is set to `true` when `config.yaml` contains any inline `contexts:`, `profiles:`, minimal-format profile fields (`agent:`, `secret:`, `env:`, `sandbox:`, `yolo:`), sandbox fields (`capabilities`, `sandboxes`, `custom_guards`, `guard_types`, `never_allow`, `never_allow_env`, `scrub_env`), or provisioning fields (`plugins`, `mcp_servers`, `hooks`).
    *   Also, if `!cfg.ProfileSchema` (i.e. `config.yaml` used `contexts:` or `default_context:`), `cfg.LegacyMonolithic` is `true`.
3.  **Merge `sandbox.yaml` (if present):**
    *   If `config.yaml` also declared any sandbox section (`capabilities`, `sandboxes`, `custom_guards`, `guard_types`, `never_allow`, `never_allow_env`, `scrub_env`), return `ErrDuplicateConfigSection` (`"sandbox settings are defined in both config.yaml and sandbox.yaml; run 'aide config migrate --write' to consolidate"`).
4.  **Merge `provisioning.yaml` (if present):**
    *   If `config.yaml` also declared any provisioning section (`plugins`, `mcp_servers`, `hooks`), return `ErrDuplicateConfigSection` (`"provisioning settings are defined in both config.yaml and provisioning.yaml; run 'aide config migrate --write' to consolidate"`).
5.  **Merge `profiles/*.yaml` (if directory exists):**
    *   Read entries in lexical order.
    *   If `cfg.Contexts[name]` already exists (defined inline in `config.yaml`), return `ErrDuplicateProfile` (`"profile %q is defined in both config.yaml and profiles/%s.yaml; run 'aide config migrate --write' to consolidate"`).
    *   When any `profiles/*.yaml` file is loaded and `config.yaml` did not use legacy `contexts:`, `cfg.ProfileSchema` is `true`.
6.  **Run Existing Post-Load Validations:**
    *   Project override discovery (`.aide.yaml`), `validateConfigDirs`, `validateProfileDirs`, `validateCredentials`, `ValidatePlugins`, `ValidateHooks` all run on the merged `Config` exactly as before.

### 3. Writing & Byte-Diff Optimization (`WriteConfig` / `WriteConfigTo`)

*   `ErrLegacyMonolithicConfig`: Returned by `WriteConfigTo` if `cfg.LegacyMonolithic` is `true`.
*   **Fresh in-memory configs** constructed by CLI commands or tests (where `cfg.LegacyMonolithic == false`) write directly to the split layout under `configDir = filepath.Dir(path)`:
    1.  **Validate before writing:** Run full post-load validation on the in-memory `cfg` (including `validateConfigDirs`, `validateProfileDirs`, `validateCredentials`, `ValidatePlugins`, `ValidateHooks`) before touching disk.
    2.  **`profiles/<name>.yaml`:**
        *   For each profile `name, ctx` in `cfg.Contexts`: marshal `ctx` using the `profiles:` schema (`config_dir_name` / `config_dir`) and write to ` filepath.Join(configDir, "profiles", name+".yaml")` via `writeIfChanged(targetPath, data)`.
        *   If `profiles/` exists on disk, any existing `profiles/<old>.yaml` whose `<old>` is no longer in `cfg.Contexts` is removed.
    3.  **`sandbox.yaml`:**
        *   Marshal `SandboxFile` from `cfg`. If non-empty, write via `writeIfChanged(filepath.Join(configDir, "sandbox.yaml"), data)`. If empty and `sandbox.yaml` exists on disk, remove `sandbox.yaml`.
    4.  **`provisioning.yaml`:**
        *   Marshal `ProvisioningFile` from `cfg`. If non-empty, write via `writeIfChanged(filepath.Join(configDir, "provisioning.yaml"), data)`. If empty and `provisioning.yaml` exists on disk, remove `provisioning.yaml`.
    5.  **`config.yaml` (`path`):**
        *   Marshal `CoreConfig` (`default_profile`, `agents`, `preferences`, `statusline`) and write via `writeIfChanged(path, data)`.
*   **`writeIfChanged(path, data)`:**
    *   Reads `existing, err := os.ReadFile(path)`. If `err == nil && bytes.Equal(existing, data)`, returns `nil` without touching the file or its mtime. Otherwise calls `fsutil.AtomicWrite(path, data)`.

### 4. Provisioning Config Hash (`internal/provision/confighash.go`)

*   Currently, `provision.ConfigHash(cfgPath)` hashes only `config.yaml` to detect drift in `aide which` and `aide sync`.
*   With the split layout, editing `provisioning.yaml` or `profiles/work.yaml` changes the desired provisioning state without changing `config.yaml`.
*   **Update `provision.ConfigHash(cfgPath)`:**
    *   Compute a deterministic composite SHA-256 over `config.yaml` plus (when present in `filepath.Dir(cfgPath)`) `sandbox.yaml`, `provisioning.yaml`, and sorted `profiles/*.yaml` (feeding `relative-filename + "\x00" + bytes + "\x00"` into the hash). If only `config.yaml` exists and no split files exist, hash `config.yaml` identically or deterministically so `RehashConfig` updates `managed.json` during migration.

### 5. Migration & Periodic Nudge

#### 5.1 `aide config migrate`
*   `aide config migrate` performs:
    1.  Existing `contexts:` $\rightarrow$ `profiles:`, credential extraction, and `--adopt` transformations.
    2.  Minimal-format promotion (if `config.yaml` is flat minimal format, promotes the single profile to `profiles/default.yaml` and sets `default_profile: default` in `config.yaml`).
    3.  Splits inline `profiles:` into `profiles/<name>.yaml`, sandbox fields into `sandbox.yaml` (if non-empty), provisioning fields into `provisioning.yaml` (if non-empty), and leaves global fields in `config.yaml`.
*   **Preview mode (`aide config migrate` without `--write`):**
    *   Prints each file that would be written (`=== config.yaml ===`, `=== sandbox.yaml ===`, `=== provisioning.yaml ===`, `=== profiles/<name>.yaml ===`) along with the migration summary.
*   **Apply mode (`aide config migrate --write`):**
    *   Backs up the original `config.yaml` to `config.yaml.bak` (or timestamped `.bak-<ts>`), writes all split files atomically, and updates `managed.json`'s `ConfigHash` via `provision.RehashConfig` so no false drift is reported.

#### 5.2 Periodic Nudge (7-day Cooldown) & Mutation Prompt
*   **Nudge State File:** `statedir.Path(home, "migrate-nudge.json")` stores `{"last_nudged_at": "2026-11-01T..."}`.
*   **On Read / Launch:** When `cfg.LegacyMonolithic` is `true` and stderr is a TTY, if `last_nudged_at` is missing or older than 7 days, print a one-line notice to stderr:
    `Note: ~/.config/aide/config.yaml uses the legacy single-file layout. Run 'aide config migrate --write' to split profiles and settings into modular files.`
    and update `last_nudged_at`.
*   **On Mutating Commands:** Before a CLI command saves mutations via `config.WriteConfig(cfg)`, if `cfg.LegacyMonolithic` is `true`:
    *   If stdin/stderr is an interactive TTY, prompt: `Config uses the legacy single-file layout and must be migrated before saving changes. Migrate now? (original kept as config.yaml.bak) [y/N]:`
    *   If confirmed, run the split migration (backing up `config.yaml.bak` and clearing `cfg.LegacyMonolithic`), then proceed with `WriteConfig(cfg)`.
    *   If declined or non-interactive, return an error: `refusing to write config: legacy single-file config must be migrated first (run 'aide config migrate --write')`.
