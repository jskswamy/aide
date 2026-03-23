# Guard System Polish — 9 Systemic Fixes

**Date:** 2026-03-23
**Status:** Approved
**Applies to:** `feature/guards-as-seatbelt-modules` branch

## Problem

The guard system implementation (22 commits, 25 guards) has 35 issues found by 5 parallel verification agents. Rather than fixing each individually, root cause analysis identified 9 systemic patterns that address them.

## Root Cause Patterns

| Pattern | Issues caused | Root cause |
|---------|-------------|------------|
| A | C1, M3 | Dual NetworkMode types (string vs int), missing GOOS wiring |
| B | C1, C4, C5, M3 | No Context validation before profile generation |
| C | H1, H2, H5, M1, M2, M4, M8, M10 | CLI commands don't understand config state machine |
| D | H3, H4 | Config serialization leaks SandboxRef.Inline wrapper |
| E | U1-U6, M5 | No guard description style convention |
| F | C2, C3, M6 | File vs directory ambiguity in deny helpers |
| G | D1-D4 | Spec written before implementation, never reconciled |
| H | T1-T4, M7, M11 | Test gaps from rapid subagent implementation |
| — | M9 | Deprecated backward-compat wrappers still in codebase |
| I | All | Guards in wrong package (`modules/` instead of `guards/`) |

## Fix 1: Context Validation + Standardized ValidationResult

### ValidationResult

A shared validation type used across all 5 validation sites:

```go
// pkg/seatbelt/validation.go (new file)

type ValidationResult struct {
    Errors   []string
    Warnings []string
}

func (r *ValidationResult) AddError(format string, args ...interface{})
func (r *ValidationResult) AddWarning(format string, args ...interface{})
func (r *ValidationResult) Err() error    // returns first error or nil
func (r *ValidationResult) OK() bool      // len(Errors) == 0
func (r *ValidationResult) Merge(other ValidationResult)
```

All validation sites migrate to `ValidationResult`:
1. `Context.Validate()` (new)
2. `PolicyFromConfig` in `policy.go`
3. `ValidateSandboxConfigDetailed` in `policy.go`
4. `ValidateCustomGuard` in `guard_custom.go`
5. CLI commands (guard/unguard)

### Context.Validate

```go
func (c *Context) Validate() *ValidationResult {
    r := &ValidationResult{}
    if c.HomeDir == "" {
        r.AddError("context: HomeDir is required for guard path resolution")
    }
    if c.GOOS == "" {
        r.AddError("context: GOOS is required for OS-aware guards")
    }
    return r
}
```

### generateSeatbeltProfile safety

```go
func generateSeatbeltProfile(policy Policy) (string, error) {
    // Always set GOOS
    goos := runtime.GOOS

    // Validate base guard is present
    hasBase := false
    for _, name := range policy.Guards {
        if name == "base" { hasBase = true; break }
    }
    if !hasBase {
        return "", fmt.Errorf("guard 'base' is required but not in Guards list")
    }

    // Build context and validate
    homeDir, _ := os.UserHomeDir()
    // ... WithContext sets all fields including c.GOOS = goos
    // ... call ctx.Validate(), return error if not OK
}
```

## Fix 2: Smart Config Mutation

### GuardConfigOps

New functions in `internal/sandbox/guard_config.go`:

```go
// EffectiveGuards resolves the active guard set for a sandbox config.
// Applies: defaults → guards (override) or guards_extra (extend) → unguard (remove)
func EffectiveGuards(cfg *config.SandboxPolicy) []string

// EnableGuard adds a guard to the config, handling state:
// - meta-guard name (cloud, all-default) → error: "use concrete guard names"
// - guards: set → append to guards:
// - guards: not set → append to guards_extra:
// - already active → warning
// - named profile → error
func EnableGuard(cfg *config.SandboxPolicy, name string) *seatbelt.ValidationResult

// DisableGuard removes a guard from the config:
// - meta-guard name (cloud, all-default) → error: "use concrete guard names"
// - always type → error
// - in guards: → remove from guards:
// - in guards_extra: → remove from guards_extra:
// - otherwise → add to unguard:
// - already inactive → warning
func DisableGuard(cfg *config.SandboxPolicy, name string) *seatbelt.ValidationResult
```

### CLI commands become thin wrappers

```go
// aide sandbox guards
activeSet := EffectiveGuards(resolvedSandboxConfig)
// use activeSet for STATUS column

// aide sandbox guard docker
result := EnableGuard(sandboxConfig, "docker")
// show result.Warnings, return result.Err()

// aide sandbox unguard browsers
result := DisableGuard(sandboxConfig, "browsers")
// show result.Warnings, return result.Err()
```

### Named profile handling

`EnableGuard` and `DisableGuard` operate on `*config.SandboxPolicy`. The CLI resolves the sandbox config first:
- Inline or default → mutate directly
- Named profile reference → error: "context uses sandbox profile 'strict'; modify the profile directly or convert to inline sandbox"
- Disabled → error: "sandbox is disabled for this context"

### Config write path

Fix `ensureInlineSandbox` (or replace it) to write flat sandbox policy YAML:

```yaml
# Before (broken):
sandbox:
  inline:
    unguard:
      - browsers

# After (correct):
sandbox:
  unguard:
    - browsers
```

The fix: add a custom `MarshalYAML` method on `SandboxRef` that flattens the inline policy. When `Inline` is set and `ProfileName` is empty:

```go
func (r SandboxRef) MarshalYAML() (interface{}, error) {
    if r.Disabled {
        return false, nil
    }
    if r.ProfileName != "" {
        return map[string]string{"profile": r.ProfileName}, nil
    }
    if r.Inline != nil {
        // Marshal the SandboxPolicy directly (flat), not wrapped in "inline:"
        return r.Inline, nil
    }
    return nil, nil
}
```

This produces clean YAML without the `inline:` wrapper. The existing `UnmarshalYAML` already handles the flat form (it falls back to parsing as inline policy), so round-tripping works.

## Fix 3: Typed Deny Helpers

Replace generic helpers with explicit file/directory variants:

```go
// pkg/seatbelt/guards/helpers.go (new file, replaces helpers in guard_cloud.go)

// DenyDir denies read+write to a directory tree using (subpath ...).
func DenyDir(path string) []seatbelt.Rule {
    return []seatbelt.Rule{
        seatbelt.Raw(fmt.Sprintf(`(deny file-read-data (subpath "%s"))`, path)),
        seatbelt.Raw(fmt.Sprintf(`(deny file-write* (subpath "%s"))`, path)),
    }
}

// DenyFile denies read+write to a single file using (literal ...).
func DenyFile(path string) []seatbelt.Rule {
    return []seatbelt.Rule{
        seatbelt.Raw(fmt.Sprintf(`(deny file-read-data (literal "%s"))`, path)),
        seatbelt.Raw(fmt.Sprintf(`(deny file-write* (literal "%s"))`, path)),
    }
}

// AllowReadFile allows reading a single file using (literal ...).
func AllowReadFile(path string) seatbelt.Rule {
    return seatbelt.Raw(fmt.Sprintf(`(allow file-read* (literal "%s"))`, path))
}

// envOverridePath returns env var value if set, otherwise defaultPath.
func envOverridePath(ctx *seatbelt.Context, envKey, defaultPath string) string {
    if val, ok := ctx.EnvLookup(envKey); ok && val != "" {
        return val
    }
    return defaultPath
}

// splitColonPaths splits colon-separated paths, skipping empty segments.
func splitColonPaths(s string) []string {
    parts := strings.Split(s, ":")
    var result []string
    for _, p := range parts {
        p = strings.TrimSpace(p)
        if p != "" {
            result = append(result, p)
        }
    }
    return result
}
```

Remove dead code: `denyPathRules`, `denyLiteralRules` (the slice-based variants that had the empty-home bug).

Each guard updated to call the right helper:

| Guard | Path | Helper |
|-------|------|--------|
| cloud-aws credentials | `~/.aws/credentials` | `DenyFile` |
| cloud-aws config | `~/.aws/config` | `DenyFile` |
| cloud-aws sso/cache | `~/.aws/sso/cache` | `DenyDir` |
| cloud-aws cli/cache | `~/.aws/cli/cache` | `DenyDir` |
| cloud-oci (default) | `~/.oci` | `DenyDir` |
| cloud-oci (env override) | file path | `DenyFile` |
| vault | `~/.vault-token` | `DenyFile` |
| terraform credentials | `~/.terraform.d/credentials.tfrc.json` | `DenyFile` |
| terraform rc | `~/.terraformrc` | `DenyFile` |
| kubernetes | `~/.kube/config` | `DenyFile` |
| docker | `~/.docker/config.json` | `DenyFile` |
| npm | `~/.npmrc`, `~/.yarnrc` | `DenyFile` |
| netrc | `~/.netrc` | `DenyFile` |
| ssh-keys | `~/.ssh` | `DenyDir` |
| browsers | each browser dir | `DenyDir` |
| password-managers dirs | `~/.config/op`, etc. | `DenyDir` |
| password-managers files | `~/.gnupg/secring.gpg` | `DenyFile` |
| aide-secrets | `~/.config/aide/secrets` | `DenyDir` |
| cloud-gcp | `~/.config/gcloud` | `DenyDir` |
| cloud-azure | `~/.azure` | `DenyDir` |
| cloud-digitalocean | `~/.config/doctl` | `DenyDir` |
| github-cli | `~/.config/gh` | `DenyDir` |
| vercel | `~/.config/vercel` | `DenyDir` |

## Fix 4: Description Style Guide + Rewrite

### Convention

- **Deny guards (default/opt-in):** "Blocks access to <what's protected>"
- **Allow guards (always):** "<What it enables> for agent operation"
- No jargon (no "Mach services", "subpath", "literal", "deny default")
- No raw paths in descriptions
- No internal field names in CLI messages

### Guard description rewrites

| Guard | New description |
|-------|----------------|
| `base` | Sandbox foundation — blocks all access unless explicitly allowed |
| `system-runtime` | System binaries, devices, and OS services for agent operation |
| `network` | Network access for agent operation |
| `filesystem` | Project directory (read-write) and home directory (read-only) access |
| `keychain` | macOS Keychain access for authentication and certificates |
| `node-toolchain` | Node.js package managers and build tool access |
| `nix-toolchain` | Nix store and profile access |
| `git-integration` | Git config and SSH host verification (read-only) |
| `ssh-keys` | Blocks access to SSH private keys; allows known_hosts and config |
| `cloud-aws` | Blocks access to AWS credentials and config |
| `cloud-gcp` | Blocks access to GCP credentials and config |
| `cloud-azure` | Blocks access to Azure CLI credentials |
| `cloud-digitalocean` | Blocks access to DigitalOcean CLI credentials |
| `cloud-oci` | Blocks access to Oracle Cloud CLI credentials |
| `kubernetes` | Blocks access to Kubernetes config |
| `terraform` | Blocks access to Terraform credentials |
| `vault` | Blocks access to Vault token |
| `browsers` | Blocks access to browser data (cookies, passwords, history) |
| `password-managers` | Blocks access to password manager data and GPG private keys |
| `aide-secrets` | Blocks access to aide's encrypted secrets |
| `docker` | Blocks access to Docker registry credentials |
| `github-cli` | Blocks access to GitHub CLI credentials |
| `npm` | Blocks access to npm and yarn auth tokens |
| `netrc` | Blocks access to netrc credentials |
| `vercel` | Blocks access to Vercel CLI credentials |

### CLI message rewrites

| Location | Current | New |
|----------|---------|-----|
| guard success | "Added guard %q to guards_extra for context %q" | "Guard %q enabled for context %q" |
| guard idempotent | "Guard %q is already in guards_extra for context %q" | "Guard %q is already enabled for context %q" |
| unguard success | "Added guard %q to unguard list for context %q" | "Guard %q disabled for context %q" |
| unguard idempotent | "Guard %q is already in unguard list for context %q" | "Guard %q is already disabled for context %q" |
| unguard always error | "cannot be unguarded" | "cannot be disabled" |
| guards command short | "List all guards with type, status, and paths" | "List all guards with type, status, and description" |
| types DEFAULT column | `DEFAULT` | `STATE` |

### Banner update

Replace `sandboxCountsLine` with a "protecting" summary:

```go
func sandboxProtectingLine(info *SandboxInfo) string {
    if info == nil || len(info.Protecting) == 0 {
        return ""
    }
    return "protecting: " + strings.Join(info.Protecting, ", ")
}
```

`SandboxInfo` gains:
```go
type SandboxInfo struct {
    // ...existing fields...
    Protecting []string // human-readable categories: "SSH keys", "cloud credentials", ...
}
```

The launcher populates `Protecting` by mapping active default/opt-in guard names to categories:
- `ssh-keys` → "SSH keys"
- `cloud-aws` + `cloud-gcp` + ... (all active) → "cloud credentials"
- `cloud-aws` + `cloud-gcp` (partial) → "cloud credentials (AWS, GCP)"
- `kubernetes` → "Kubernetes config"
- `terraform` → "Terraform credentials"
- `vault` → "Vault token"
- `browsers` → "browser data"
- `password-managers` → "password manager data"
- `aide-secrets` → "aide secrets"
- `docker` → "Docker credentials"
- etc.

Always-type guards are not listed in "protecting" — they're infrastructure, not protection.

Shield emoji: use `🛡` (U+1F6E1, no variation selector) for consistent terminal width.

## Fix 5: Unify NetworkMode

Remove `seatbelt.NetworkMode int` from `pkg/seatbelt/module.go`. Context uses a plain string:

```go
type Context struct {
    // ...existing fields...
    Network string // "outbound", "none", "unrestricted", or ""
    // Remove: Network NetworkMode
}
```

Network guard switches on string:

```go
func (m *networkGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
    switch ctx.Network {
    case "outbound":
        return networkOutboundRules(ctx.AllowPorts, ctx.DenyPorts)
    case "none":
        return nil
    case "unrestricted", "":
        return []seatbelt.Rule{seatbelt.Allow("network*")}
    default:
        return nil
    }
}
```

`darwin.go` WithContext does a direct copy — both are strings, no cast needed:
```go
c.Network = string(policy.Network) // policy.Network is type NetworkMode = string
```

Note: `sandbox.NetworkMode` is `type NetworkMode string`. The `string()` cast is technically a no-op but makes the type boundary explicit. Both sides are strings.

Remove from `module.go`: `NetworkMode int`, `NetworkOpen`, `NetworkOutbound`, `NetworkNone` constants.

## Fix 6: Test Coverage

**Test file locations:**
- `pkg/seatbelt/context_test.go` — EnvLookup edge cases
- `pkg/seatbelt/validation_test.go` — ValidationResult tests
- `pkg/seatbelt/guards/registry_test.go` — dedup, meta-guard expansion
- `pkg/seatbelt/guards/guard_cloud_test.go` — KUBECONFIG edge cases
- `pkg/seatbelt/guards/guard_browsers_test.go` — empty GOOS
- `pkg/seatbelt/guards/guard_custom_test.go` — zero paths, multi-colon env
- `internal/sandbox/darwin_test.go` — safety tests, round-trip tests
- `internal/sandbox/policy_test.go` — meta-guard in guards_extra/unguard, dedup
- `internal/sandbox/guard_config_test.go` — EnableGuard/DisableGuard/EffectiveGuards

### Safety tests
- Empty Guards list → `generateSeatbeltProfile` returns error
- Empty HomeDir → `Context.Validate()` returns error
- Config→profile round-trip: unguard ssh-keys → `.ssh` deny absent
- Config→profile round-trip: guard docker → `.docker` deny present

### Meta-guard expansion tests
- `guards_extra: [cloud]` → all 5 providers active
- `unguard: [cloud]` → all 5 providers removed

### Edge case tests
- `ResolveActiveGuards` duplicate names → deduplicated
- `guards: [ssh-keys, ssh-keys]` → appears once
- KUBECONFIG `/a::/b` → empty segment skipped
- `EnvLookup` duplicate keys → first-wins
- Browser guard empty GOOS → Context.Validate catches it
- Only always-guards → valid profile with `(deny default)`
- `ValidateCustomGuard` zero paths → error
- `unguard: [nonexistent]` → error mentioning "unguard"
- `ValidateSandboxConfigDetailed` unknown guard in unguard → error
- Custom guard env multi-colon → falls back to default

## Fix 7: Spec + Docs Reconciliation

### Spec updates (`2026-03-22-sandbox-guards-design.md`)
- Config YAML: nest `allow_ports`/`deny_ports` under `network:`
- CLI output: PATHS → DESCRIPTION column
- Remove `types show/add/remove` subcommands
- Note: `aide sandbox guard/unguard` accept concrete names only, not meta-guards
- Context: `Network` is `string` not `NetworkMode int`

### docs/sandbox.md updates
- Replace Writable/Readable/Denied table with guard-based description
- Lead with `guards:`/`guards_extra:`/`unguard:` config
- Add guard CLI commands
- Update library example to guard API
- Update "Available modules" to reference guard constructors

### Code comment updates
- `ValidateSandboxConfigDetailed`: note writable/readable retained for backward compat
- `ResolveActiveGuards`: "unknown names silently skipped — callers should validate first"

## Fix 8: Remove Deprecated Backward-Compat Wrappers

Delete all deprecated aliases and compat types:

**Functions to remove:**
- `Base()` → callers use `BaseGuard()`
- `SystemRuntime()` → `SystemRuntimeGuard()`
- `Network(mode)` → `NetworkGuard()` (reads from ctx)
- `NetworkWithPorts(mode, opts)` → `NetworkGuard()` (reads from ctx)
- `Filesystem(cfg)` → `FilesystemGuard()` (reads from ctx)
- `KeychainIntegration()` → `KeychainGuard()`
- `NodeToolchain()` → `NodeToolchainGuard()`
- `NixToolchain()` → `NixToolchainGuard()`
- `GitIntegration()` → `GitIntegrationGuard()`

**Types to remove:**
- `networkModuleCompat` struct
- `filesystemModuleCompat` struct
- `FilesystemConfig` struct
- `PortOpts` struct
- `NetworkMode` type alias (in modules package)
- `NetworkModeOpen`/`NetworkModeOutbound`/`NetworkModeNone` constants
- `NetworkOpen`/`NetworkOutbound`/`NetworkNone` vars

**Verify no remaining callers:**
- `darwin.go` already uses guard registry
- Tests already use guard constructors
- `claude.go` (`ClaudeAgent()`) is an agent module, not affected

Grep for all removed function/type names to catch any remaining references.

Run full test suite after removal to confirm no breakage before proceeding to the next fix.

## Fix 9: Move Guards to `pkg/seatbelt/guards/`

Guards are policy decisions, not rendering primitives. They belong in their own package, parallel to the seatbelt rendering engine. Since `pkg/seatbelt/` is already public for custom sandbox profile building, guards should be too.

### New package structure

```
pkg/seatbelt/                    # rendering engine (Module, Guard, Context, Rule, Profile)
  module.go                      # Guard interface, Context
  validation.go                  # ValidationResult (Fix 1)
  profile.go, render.go, path.go # rendering

pkg/seatbelt/guards/             # NEW: all guard implementations + registry
  guard_base.go
  guard_system_runtime.go
  guard_network.go
  guard_filesystem.go
  guard_keychain.go
  guard_node_toolchain.go
  guard_nix_toolchain.go
  guard_git_integration.go
  guard_ssh_keys.go
  guard_cloud.go
  guard_kubernetes.go
  guard_terraform.go
  guard_vault.go
  guard_browsers.go
  guard_password_managers.go
  guard_aide_secrets.go
  guard_sensitive.go              # docker, github-cli, npm, netrc, vercel (opt-in guards)
  guard_custom.go
  helpers.go
  registry.go
  (+ corresponding test files)

pkg/seatbelt/modules/            # agent modules only (not guards)
  claude.go
```

### Import path changes

All imports of `github.com/jskswamy/aide/pkg/seatbelt/modules` that reference guards change to `github.com/jskswamy/aide/pkg/seatbelt/guards`:

```go
// Before:
import "github.com/jskswamy/aide/pkg/seatbelt/modules"
modules.AllGuards()
modules.SSHKeysGuard()

// After:
import "github.com/jskswamy/aide/pkg/seatbelt/guards"
guards.AllGuards()
guards.SSHKeysGuard()
```

Files that import guards:
- `internal/sandbox/darwin.go` — `guards.ResolveActiveGuards`
- `internal/sandbox/policy.go` — `guards.DefaultGuardNames`, `guards.GuardByName`, etc.
- `internal/sandbox/sandbox.go` — `guards.DefaultGuardNames`
- `internal/sandbox/guard_config.go` — `guards.GuardByName`, `guards.GuardsByType`
- `cmd/aide/commands.go` — `guards.AllGuards`, `guards.GuardByName`, `guards.DefaultGuardNames`

Files that import only agent modules (stay as `modules`):
- `internal/sandbox/darwin.go` — `modules.ClaudeAgent()` (via policy.AgentModule)
- `internal/launcher/agentcfg.go` — `modules.ClaudeAgent()`

### Why `pkg/` not `internal/`

`pkg/seatbelt/` is already public. Users building custom sandbox profiles should be able to:
- Use specific guards in their own compositions
- Inspect which guards exist and what they protect
- Build custom tooling around the guard registry

## Implementation Order

Fixes should be applied in this order due to dependencies:

1. **Fix 9** (Package restructure) — move files first, then all subsequent fixes work on the new locations. This avoids touching files twice (once in old location, once to move).
2. **Fix 5** (Unify NetworkMode) — changes Context struct, must happen before Fix 1
3. **Fix 8** (Remove deprecated) — cleans up before other changes touch the same files. Run full test suite after removal before proceeding.
4. **Fix 1** (Context validation + ValidationResult) — foundation for Fixes 2, 3, 4
5. **Fix 3** (Typed deny helpers) — must happen before Fix 4 updates guard files
6. **Fix 4** (Descriptions + CLI messages + banner) — touches all guard files and commands.go
7. **Fix 2** (Smart config mutation) — depends on Fix 1 for ValidationResult
8. **Fix 6** (Test coverage) — tests the fixed code
9. **Fix 7** (Spec + docs) — documents the final state

**Why Fix 9 is first:** every other fix touches guard files. If we move files after making changes, we'd be doing double work. Move first, then all edits happen in the final location.

## Files Changed

| File | Fixes |
|------|-------|
| `pkg/seatbelt/module.go` | 1, 5 (Context changes, remove NetworkMode int) |
| `pkg/seatbelt/validation.go` | 1 (new: ValidationResult) |
| `pkg/seatbelt/guards/` (new package) | 9 (moved from `pkg/seatbelt/modules/`) |
| `pkg/seatbelt/guards/helpers.go` | 3 (new: DenyDir, DenyFile, AllowReadFile) |
| `pkg/seatbelt/guards/guard_*.go` (all 25) | 3, 4 (typed helpers, description rewrites) |
| `pkg/seatbelt/guards/guard_network.go` | 5, 8 (string switch, remove compat) |
| `pkg/seatbelt/guards/guard_filesystem.go` | 8 (remove compat) |
| `pkg/seatbelt/guards/guard_cloud.go` | 3 (move helpers to helpers.go, fix OCI) |
| `pkg/seatbelt/guards/guard_custom.go` | 1 (use ValidationResult) |
| `pkg/seatbelt/guards/registry.go` | 7 (comment clarification) |
| `pkg/seatbelt/modules/claude.go` | 9 (stays, only agent modules remain here) |
| `internal/sandbox/sandbox.go` | 5, 9 (import path change) |
| `internal/sandbox/darwin.go` | 1, 5, 9 (GOOS fix, validation, import path) |
| `internal/sandbox/policy.go` | 1, 2, 9 (ValidationResult, guard config ops, import path) |
| `internal/sandbox/guard_config.go` | 2 (new: EffectiveGuards, EnableGuard, DisableGuard) |
| `internal/ui/banner.go` | 4 (protecting line, shield emoji fix) |
| `cmd/aide/commands.go` | 2, 4, 9 (thin CLI wrappers, message rewrites, import path) |
| `internal/config/schema.go` | 2 (SandboxRef marshaling fix) |
| `internal/launcher/agentcfg.go` | unchanged (imports only `modules.ClaudeAgent`, not guards) |
| `docs/superpowers/specs/2026-03-22-sandbox-guards-design.md` | 7 |
| `docs/sandbox.md` | 7 |
| Test files (multiple) | 6, 9 (16 new tests + moved test files) |

## Appendix: Full Issue Inventory (35 issues)

Every issue mapped to its fix pattern.

### Critical (5)

| ID | Issue | Fix |
|----|-------|-----|
| C1 | `ctx.GOOS` never set in `darwin.go` — browsers guard emits empty rules | Fix 1 (Context validation + GOOS assignment) |
| C2 | Dead code `denyPathRules`/`denyLiteralRules` with broken relative paths | Fix 3 (remove dead code, replace with DenyDir/DenyFile) |
| C3 | OCI guard uses `subpath` for file path when `OCI_CLI_CONFIG_FILE` env set | Fix 3 (use DenyFile for file paths) |
| C4 | Empty Guards list → no `(deny default)` → seatbelt allows everything | Fix 1 (base guard required check in generateSeatbeltProfile) |
| C5 | Empty HomeDir → relative paths in seatbelt rules → denies nothing | Fix 1 (Context.Validate rejects empty HomeDir) |

### High (5)

| ID | Issue | Fix |
|----|-------|-----|
| H1 | `guards list` STATUS ignores context config | Fix 2 (EffectiveGuards for STATUS) |
| H2 | `aide sandbox guard` corrupts named profile references | Fix 2 (EnableGuard errors on named profiles) |
| H3 | Config writes `inline:` wrapper visible to users | Fix 2 (SandboxRef.MarshalYAML flattens inline) |
| H4 | `Writable`/`Readable` config fields parsed but never applied | Fix 2 (add deprecation warning in ValidateSandboxConfigDetailed) |
| H5 | `aide sandbox guard` appends to `guards_extra` when `guards:` set — silently ignored | Fix 2 (EnableGuard appends to correct field based on state) |

### UX (6)

| ID | Issue | Fix |
|----|-------|-----|
| U1 | Guard descriptions use jargon ("(version 1), (deny default)", "Mach services") | Fix 4 (description rewrite) |
| U2 | CLI messages leak internal field names ("guards_extra", "unguard list") | Fix 4 (message rewrite) |
| U3 | Banner shows "guards: 20 active" — uninformative, shield emoji misaligned | Fix 4 (protecting line, emoji fix) |
| U4 | `DEFAULT` column in types table conflicts with "default" type name | Fix 4 (rename to STATE) |
| U5 | `guards` short description promises "paths" not shown | Fix 4 (rewrite to "description") |
| U6 | "cannot be unguarded" vs "cannot be disabled" — inconsistent | Fix 4 (standardize on "disabled") |

### Documentation (4)

| ID | Issue | Fix |
|----|-------|-----|
| D1 | `docs/sandbox.md` uses old Writable/Readable/Denied model | Fix 7 |
| D2 | Spec YAML shows flat `allow_ports`/`deny_ports`; actual nests under `network:` | Fix 7 |
| D3 | Spec shows `types show/add/remove` — not implemented | Fix 7 |
| D4 | Spec output shows PATHS column — implementation shows DESCRIPTION | Fix 7 |

### Test Gaps (4)

| ID | Issue | Fix |
|----|-------|-----|
| T1 | No end-to-end config→profile round-trip test | Fix 6 |
| T2 | Meta-guard expansion in `guards_extra`/`unguard` not tested | Fix 6 |
| T3 | Profile with only always-guards not tested | Fix 6 |
| T4 | ResolveActiveGuards deduplication not tested | Fix 6 |

### Minor (11)

| ID | Issue | Fix |
|----|-------|-----|
| M1 | Always-guard in `guards:` silently dropped without warning | Fix 2 (EnableGuard warns) |
| M2 | `unguard` on opt-in guard is no-op (but STATUS changes once Fix 2 is applied) | Fix 2 (DisableGuard warns if already inactive) |
| M3 | Empty/unknown GOOS fallback for browsers | Fix 1 (Context.Validate catches empty GOOS) |
| M4 | Bare `~` expansion (only `~/` works) | Fix 2 (ResolvePaths handles bare `~`) |
| M5 | Column widths in CLI tables fixed at 20 chars | Fix 4 (widen or compute dynamically) |
| M6 | Custom guard env multi-path fallback silent | Fix 3 (add comment documenting behavior) |
| M7 | Duplicate keys in EnvLookup first-wins semantics undocumented | Fix 6 (add test documenting behavior) |
| M8 | `PolicyFromConfig` doesn't set `policy.Env` | Fix 2 (document that callers set Env) |
| M9 | Registry `builtinGuards` mutable var | Fix 8 (no change — document as acceptable) |
| M10 | Template `missingkey=error` gives unhelpful messages | Fix 2 (improve error wrapping) |
| M11 | Test name `TestPolicyFromConfig_DeniedAndDeniedExtra` understates coverage | Fix 6 (rename or split test) |
