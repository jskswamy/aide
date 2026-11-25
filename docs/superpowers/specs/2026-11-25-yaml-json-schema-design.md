# YAML JSON Schemas & Editor Hints for `aide` Config Files

## Evidence & Review Records

- **Analyzed Scope & Paths**:
  - `internal/config/schema.go` (`Config`, `CoreConfig`, `Context`, `SandboxPolicy`, `SandboxRef`, `NetworkPolicy`, `CapabilityDef`, `CustomGuard`, `GuardType`, `ProjectOverride`, `Preferences`, `StatuslineConfig`, `ModuleConfig`, `PluginMap`, `MCPServerMap`, `HooksMap`, `MatchRule`)
  - `internal/config/split.go` (`CoreConfig`, `SandboxFile`, `ProvisioningFile`, `loadSplitFiles`)
  - `internal/config/profiles.go` (`EncodeProfileYAML`, `DecodeProfileYAML`, `profileDirKeys`: `config_dir_name`/`config_dir`)
  - `internal/config/gateway.go` (`GatewayConfig`)
  - `internal/config/credentials.go` (`CredentialBinding`, `CredentialRoute`)
  - `internal/config/writer.go` (`WriteConfig`, `WriteConfigTo`, `writeIfChanged`)
  - `internal/config/project_writer.go` (`WriteProjectOverride`, `WriteProjectOverrideWithTrust`)
  - `internal/config/paths.go` (`Dir`, `FilePath`, `ProfileFilePath`)
  - `internal/yamlnode/yamlnode.go` (`Marshal`, `Merge`)
  - `cmd/aide/config.go` (`configCmd`)
  - `.goreleaser.yml`, `.github/workflows/release.yml`, `.github/workflows/checks.yml`, `go.mod`, `flake.nix`
- **Code Revision**: `e01d76a` (direct source inspection; unrelated uncommitted files in working tree: `docs/designs/aide-doctor.md`, `docs/designs/SITEMAP.md`, and doctor review reports)
- **Coverage Gaps**: None for config schema/writer/CLI/CI boundaries. Built-in capability names and gateway preset names used for editor completion hints should be verified against `internal/capability/` and `internal/gatewaypreset/` during implementation.
- **Review Records**:
  - **Review mode**: Lightweight — user selected Ponytail only | Scope: new `schemas/` embed package, writer modeline hook, `aide config schema` subcommand, `.goreleaser.yml` publishing; no sandbox/security boundary or dependency changes
  - **EGM review**: Skipped (user-selected Lightweight mode)
  - **Ponytail review**: Completed (proposed-design check against connected code at `e01d76a`; no implementation tests run yet)
  - **Finding dispositions**:
    1. **[Blocker / Must fix] `profile.schema.json` key mapping (`config_dir_name` / `config_dir`)** — **Fixed in spec**: explicitly specified `EncodeProfileYAML` key translation (`profile` → `config_dir_name`, `profile_dir` → `config_dir`) in both `profile.schema.json` and the reflection drift test.
    2. **[Blocker / Must fix] `yamlnode.Merge` drops leading `$schema` comment if first YAML key is removed** — **Fixed in spec**: `writer.go` / `project_writer.go` extract any existing leading `# yaml-language-server:` line from `existing` *before* `yamlnode.Merge` and restore it if dropped.
    3. **[Major / Should fix] `--local` `file://` URI encoding for paths with spaces/special chars** — **Fixed in spec**: construct local URIs with `(&url.URL{Scheme: "file", Path: filepath.ToSlash(absPath)}).String()`.
    4. **[Nit / Nice to have] Deduplicate modeline scanning** — **Fixed in spec**: `HasModeline`, `EnsureModeline`, and `SetModeline` share a single internal leading-header parser.
  - **Review status**: Approved (user approved Lightweight Ponytail review and spec on 2026-11-25)

---

## Intent & Why

When editing `aide` YAML configuration files (such as `~/.config/aide/profiles/work.yaml` or dotfiles-managed `modules/.../profiles/work.yaml`) in editors with `yaml-language-server` (Neovim, VS Code, Helix, Zed), SchemaStore's filename globbing falsely matches `profiles/*.yaml` against an unrelated schema expecting `apiVersion`. Every valid `aide` key is flagged as an error, and editors cannot offer completion or hover documentation for valid sections and fields.

Providing official JSON Schemas for all 5 `aide` YAML file shapes, distributing them both **embedded in the `aide` binary** (for offline/local use) and **published via GitHub/CI** (for portable dotfiles, Nix repos, and shared `.aide.yaml` files), and stamping a `# yaml-language-server: $schema=...` header at the top of `aide` YAML files eliminates false-positive diagnostics and gives users first-class editor autocomplete, hover docs, and validation.

---

## Goals, Non-Goals, and Anti-Goals

### Goals
1. Ship accurate JSON Schema (Draft 2020-12) definitions for all 5 `aide` YAML config file types:
   - `config.schema.json` — `~/.config/aide/config.yaml` (`CoreConfig`)
   - `profile.schema.json` — `~/.config/aide/profiles/<name>.yaml` (`Context`)
   - `sandbox.schema.json` — `~/.config/aide/sandbox.yaml` (`SandboxFile`)
   - `provisioning.schema.json` — `~/.config/aide/provisioning.yaml` (`ProvisioningFile`)
   - `project.schema.json` — `.aide.yaml` (`ProjectOverride`)
2. Prevent schema drift from Go structs via an automated CI unit test (reflection over `yaml` struct tags + real YAML fixture validation).
3. Support **hybrid distribution**:
   - Embedded in the `aide` binary via `//go:embed` (zero runtime network dependency for local export).
   - Checked into `schemas/` on `main` (immediately consumable via `raw.githubusercontent.com`) and attached as GitHub Release assets + included in release tarballs via GoReleaser.
4. Automatically include `# yaml-language-server: $schema=<url>` on newly written/migrated config files while **preserving** any user-customized `$schema` directive.
5. Provide `aide config schema` subcommands so users can export schemas locally, inspect them, or stamp existing config files (including `--local` for offline setups).

### Non-Goals
- Submitting glob registrations to upstream SchemaStore (`schemastore.org`) — `profiles/*.yaml` is too generic globally and `.aide.yaml` can be a separate follow-up if desired.
- Validating `aide` runtime config loading with a JSON Schema engine — Go's existing `internal/config` loader and validators remain the runtime source of truth.
- Supporting legacy monolithic `config.yaml` or legacy `contexts:` / `harnesses:` keys as primary completions (deprecated aliases may be tolerated only where needed, without advertising them as recommended completions).

### Anti-Goals
- **Breaking portable configs:** Never default to machine-specific absolute paths (e.g. `/Users/<name>/.config/aide/schemas/...`) in `$schema` headers, because users store profiles in Nix/dotfiles repos and commit `.aide.yaml` to shared git repos.
- **Clobbering user overrides:** Never overwrite an existing `# yaml-language-server:` line when `aide` rewrites a config file.
- **Rewriting untouched files:** Never churn mtime or rewrite existing files on disk when no semantic change occurred (preserve `writeIfChanged` contract).
- **Adding unnecessary dependencies:** Do not introduce heavy third-party JSON Schema generator dependencies into `go.mod` (keeps `flake.nix` `vendorHash` and supply-chain surface lean).

---

## Approaches Considered

| Approach | Summary | Trade-offs |
|---|---|---|
| **A: Go codegen (`go generate`)** | Generate `schemas/*.schema.json` from Go structs using `invopop/jsonschema` or a custom reflector, plus CI staleness diff. | Auto-updates plain struct fields, but ~7 core config types (`Context`, `SandboxRef`, `NetworkPolicy`, `PluginMap`, `MCPServerMap`, `HooksMap`, `GatewayConfig`) use custom `UnmarshalYAML` unions that still require manual schema code in Go; adds a third-party dependency. |
| **B: Hand-authored `schemas/*.schema.json` + Go reflection drift test (Selected)** | Author the 5 Draft 2020-12 JSON Schemas in `schemas/`, embed them via `schemas/schemas.go`, and enforce 100% struct-tag parity + fixture validation in `go test`. | Requires updating the `.schema.json` when adding a Go config field (enforced by CI), in exchange for zero new dependencies, rich hand-written descriptions/examples/enum completions, and clean modeling of polymorphic YAML unions. |
| **C: Runtime-generated release artifacts only** | No checked-in `schemas/` files; CI generates and uploads schemas only on tagged releases. | Keeps git clean, but PR diffs don't show schema changes, `main` raw URLs don't exist, and dev/unreleased builds lack hosted schemas. |

**Selected:** **Approach B** with **Hybrid Distribution** (embedded in binary + checked into `schemas/` on `main` + published as GoReleaser release assets/tarball files).

---

## Architecture & Contracts

### 1. `schemas/` Package & Embedded Assets

Place the 5 JSON Schema files and a minimal Go embed wrapper at the repository root `schemas/` so the same files serve `//go:embed`, `raw.githubusercontent.com/jskswamy/aide/main/schemas/...`, and GoReleaser without duplication:

```
schemas/
├── schemas.go                # package schemas: //go:embed *.schema.json, Kind enum, URL & Modeline helpers
├── config.schema.json        # CoreConfig (~/.config/aide/config.yaml)
├── profile.schema.json       # Context (~/.config/aide/profiles/<name>.yaml)
├── sandbox.schema.json       # SandboxFile (~/.config/aide/sandbox.yaml)
├── provisioning.schema.json  # ProvisioningFile (~/.config/aide/provisioning.yaml)
└── project.schema.json       # ProjectOverride (.aide.yaml)
```

**Public contract (`package schemas`):**
- `Kind` typed string enum: `KindConfig`, `KindProfile`, `KindSandbox`, `KindProvisioning`, `KindProject` + `AllKinds() []Kind`.
- `FS embed.FS` embedding `*.schema.json`.
- `Filename(k Kind) string` → e.g. `"profile.schema.json"`.
- `Bytes(k Kind) ([]byte, error)` → reads embedded JSON bytes.
- `DefaultURL(k Kind) string` → `"https://raw.githubusercontent.com/jskswamy/aide/main/schemas/" + Filename(k)`.
  - *Design call:* Defaulting to `main` (rather than `releases/latest/download/`) ensures schemas resolve immediately upon merge and for unreleased/dev features, while GoReleaser also publishes version-pinned assets at `https://github.com/jskswamy/aide/releases/download/vX.Y.Z/<file>` and `.../releases/latest/download/<file>` for users who want pinned versions.
- `Modeline(target string) string` → `"# yaml-language-server: $schema=" + target`.
- `FileURI(absPath string) string` → `(&url.URL{Scheme: "file", Path: filepath.ToSlash(absPath)}).String()` (properly percent-encodes spaces/special characters in local paths).
- `HasModeline(data []byte) bool`, `ExtractModeline(data []byte) (string, bool)`, `EnsureModeline(data []byte, target string) []byte`, and `SetModeline(data []byte, target string) []byte` — all backed by a single internal leading-comment-block parser:
  - Scans only the leading comment/blank block (before the first non-comment, non-blank line; if the file starts with `---\n`, places new headers above `---`).
  - `EnsureModeline` returns `data` unchanged if a `# yaml-language-server:` directive is already present; otherwise prepends `Modeline(target) + "\n"`.
  - `SetModeline` replaces an existing `# yaml-language-server: $schema=...` line in the leading block, or prepends one if absent (used by `aide config schema stamp`).

**Schema authoring conventions (Draft 2020-12):**
- Every schema sets `"$schema": "https://json-schema.org/draft/2020-12/schema"`, `"$id"`, `"title"`, `"description"`, and `"additionalProperties": false` on root and fixed-field objects so typos are caught in the editor.
- **Profile key translation (`internal/config/profiles.go`):** `profiles/<name>.yaml` uses `EncodeProfileYAML`/`DecodeProfileYAML`, which map `Context.Profile` → `config_dir_name` and `Context.ProfileDir` → `config_dir`. `profile.schema.json` declares `config_dir_name` and `config_dir` (not the legacy `profile` / `profile_dir` keys), and the reflection drift test encodes this exact translation table.
- Polymorphic YAML types are modeled explicitly with `oneOf`:
  - `SandboxRef`: `const: false` | `string` (named sandbox profile) | `{ profile: string }` | `SandboxPolicy` object.
  - `SandboxPolicy` (in `.aide.yaml` / `sandbox.yaml`): `const: false` | object.
  - `NetworkPolicy`: `string` (`"none"`, `"outbound"`, `"unrestricted"`) | `{ mode, allow_ports, deny_ports }`.
  - `Context.mcp_servers`: `string[]` (legacy list) | `ContextOverride` (`{ only, exclude, extra }`).
  - `Context.plugins`: `ContextOverride` (`{ only, exclude, extra }`).
  - `PluginEntry`: `string[]` (marketplace) | `string` (URL-direct) | `null` (declare-only).
- Editor UX hints: include concise `description` on every property, and `examples` / `anyOf` (enum + string fallback where user-extensible) for known agents, gateway presets/APIs, network modes, statusline modules, and built-in capabilities.

### 2. Modeline Integration in Config Writers

- **`internal/config/writer.go` (`WriteConfigTo`)**:
  - When writing `config.yaml`, `sandbox.yaml`, `provisioning.yaml`, and `profiles/<name>.yaml`, `writeIfChanged` checks `existing`:
    - If `same(existing)` is true, it short-circuits before merge/write, preserving mtime and avoiding unexpected diffs.
    - Before calling `yamlnode.Merge(existing, data)`, extract any existing leading modeline via `schemas.ExtractModeline(existing)` (because `yaml.v3` attaches a line-1 comment to the first mapping key's `HeadComment`, which `yamlnode.Merge` drops if the first key is removed or replaced).
    - After `yamlnode.Merge`, if `existing` had a modeline, preserve that exact modeline (if `Merge` dropped it); otherwise apply `schemas.EnsureModeline(data, schemas.DefaultURL(kind))`.
  - `sameStructYAML` and `sameProfileYAML` already ignore YAML comments, so adding a header does not break semantic equality checks.
- **`internal/config/project_writer.go` (`WriteProjectOverride`)**:
  - Follow the exact same extract-before-merge + `EnsureModeline(..., schemas.DefaultURL(schemas.KindProject))` pattern.
  - `WriteProjectOverrideWithTrust` continues to compute the post-write hash after `WriteProjectOverride`, preserving trust-gate integrity with zero changes to trust logic.

### 3. CLI Subcommand: `aide config schema`

Add a `schema` subcommand group under `aide config` in `cmd/aide/config_schema.go` (wired from `cmd/aide/config.go`):

- **`aide config schema show <kind>`**
  - Prints the embedded JSON Schema for `config | profile | sandbox | provisioning | project` to stdout (or lists available kinds if omitted/invalid).
- **`aide config schema export [--dir <path>]`**
  - Writes all 5 embedded `*.schema.json` files atomically (`fsutil.AtomicWrite`) to `<dir>` (default: `filepath.Join(config.Dir(), "schemas")`, i.e. `~/.config/aide/schemas/`).
  - Supports `--json` / structured output via `internal/output` reporting the written paths.
- **`aide config schema stamp [--local] [--dir <path>] [--project] [--force]`**
  - Adds `# yaml-language-server: $schema=...` to existing `aide` config files in `~/.config/aide/` (`config.yaml`, `sandbox.yaml`, `provisioning.yaml`, `profiles/*.yaml`), and optionally the current project's `.aide.yaml` when `--project` is passed.
  - Default target: `schemas.DefaultURL(kind)`.
  - With `--local`: exports schemas to `--dir` (default `~/.config/aide/schemas/`) first, then stamps `schemas.FileURI(absPath)` (properly percent-encoded `file://` URI, since `yaml-language-server` requires absolute `file://` URIs and does not expand `~`).
  - Without `--force`: skips files that already have a `# yaml-language-server:` directive. With `--force`: replaces the existing `$schema` directive.
  - When stamping `.aide.yaml`, uses the same trust-preserving pattern as `WriteProjectOverrideWithTrust` (re-trust only if previously trusted; never auto-trust an untrusted file).

### 4. CI & Release Publishing

- **`.goreleaser.yml`**:
  - Include `schemas/*.schema.json` in `archives[].files` so release tarballs ship the schemas alongside `LICENSE` and `README.md`.
  - Add `release.extra_files` with `glob: schemas/*.schema.json` so every GitHub Release publishes standalone assets at `https://github.com/jskswamy/aide/releases/download/<tag>/<name>.schema.json` (and `releases/latest/download/<name>.schema.json`).
- **`.github/workflows/checks.yml`**:
  - No new job required if schema validation & drift tests run inside `go test -race ./...` (`_test.yml`), which already runs on every PR and push to `main` (Linux and macOS).

### 5. Documentation Updates

Per `AGENTS.md` doc-sync rules:
- `docs/cli-reference.md`: document `aide config schema show|export|stamp` and flags.
- `docs/configuration.md`: document the `# yaml-language-server: $schema=...` header, the 5 schema URLs, local export for offline use, and dotfiles/Nix usage.

---

## Risks & Mitigations

| Risk | Mitigation |
|---|---|
| **Schema drift** when a new Go field is added to `internal/config/` | Reflection-based drift test in `internal/config/jsonschema_drift_test.go` extracts every `yaml:"..."` tag from all 5 root structs and their nested structs, failing `go test` if any Go key is missing from the corresponding `.schema.json` or if any non-polymorphic schema key doesn't exist in Go. |
| **Invalid JSON Schema syntax or broken `$ref`s** | Unit test validates every embedded schema is valid JSON, contains valid Draft 2020-12 structure, and every internal `#/$defs/...` reference resolves. |
| **`yamlnode.Merge` strips or duplicates leading comments** | Dedicated unit tests in `internal/config/writer_test.go` and `project_writer_test.go` covering: new file gets header, existing file with custom `$schema` keeps custom header, existing file with `---` places header above `---`, repeated writes are idempotent. |
| **`.aide.yaml` trust bypass via `stamp --project`** | `stamp --project` checks pre-modification trust status via `trust.Store` and only re-trusts if the file was already trusted before stamping. |
| **`flake.nix` / `go.mod` breakage** | Zero new Go dependencies; `go.mod` and `flake.nix` `vendorHash` remain untouched. |

---

## Testing Strategy

1. **`schemas/schemas_test.go`**:
   - Every `Kind` embeds valid JSON with expected `$schema`, `$id`, and resolving `#/$defs/...` references.
   - `HasModeline`, `EnsureModeline`, and `SetModeline` handle empty files, leading comments, `---\n` document markers, existing custom `$schema` lines, and idempotency.
2. **`internal/config/jsonschema_drift_test.go`**:
   - Reflection parity test: walks `CoreConfig`, `Context`, `SandboxFile`, `ProvisioningFile`, `ProjectOverride` and all reachable nested structs; asserts every `yaml` field tag is declared in the corresponding schema's `properties` (with an explicit, documented allowlist only for custom-unmarshaled polymorphic fields like `mcp_servers`, `plugins`, `hooks`, and deprecated legacy aliases like `base_url` / `harnesses`).
   - Positive & negative fixture checks against representative `config.yaml`, `profiles/*.yaml`, `sandbox.yaml`, `provisioning.yaml`, and `.aide.yaml` documents.
3. **`internal/config/writer_test.go` & `project_writer_test.go`**:
   - Verify `WriteConfigTo` and `WriteProjectOverride` emit the expected `# yaml-language-server: $schema=...` header on new files, preserve custom `$schema` headers on update, and preserve mtime on unchanged files.
4. **`cmd/aide/config_schema_test.go`**:
   - Test `aide config schema show`, `export` (default & `--dir`), and `stamp` (default URL, `--local`, `--force`, `--project` with trust preservation).
