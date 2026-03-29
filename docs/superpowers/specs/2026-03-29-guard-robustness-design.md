# Aide Robustness Hardening

**Date:** 2026-03-29
**Status:** Approved

## Problem

The capability feature work (narrow baseline refactor, git-integration guard,
EnableGuard plumbing) introduced bugs that surfaced during real-world testing.
An audit of all 12 guards and the capability pipeline found systemic issues
across 4 areas.

### Real-World Failures Observed

1. **gh CLI blocked** — `~/.config/gh/config.yml: operation not permitted`
   even with `github` capability enabled in another context
2. **git push blocked** — `~/.ssh/known_hosts: Operation not permitted`
   because `git-remote` capability was detected but never suggested
3. **nix eval blocked** — `/etc/nix/registry.json: Operation not permitted`
   because nix guard doesn't cover system-level nix paths
4. **file command blocked on ~/go/bin/** — `go` capability detected but
   never suggested because suggestion only shows with zero caps active
5. **GPG signing blocked** — fixed in v1.4.0 via git-integration guard

### Root Causes

**Guard robustness:** 8 of 12 guards panic on nil context. 7 guards produce
wrong paths when HomeDir is empty. Symlink resolution was removed globally
(022173b) but not added to individual guards.

**Guard coverage gaps:** Nix guard missing `/etc/nix/registry.json`. System
nix paths incomplete for `nix eval`, `nix flake check`.

**Auto-suggest pipeline broken:** Detection only runs when ZERO capabilities
are active (`launcher.go:480`). The moment a user enables even one capability,
all suggestions are suppressed. This is the most common case — partially
configured users never see what they're missing.

**aide-secrets guard untested:** Zero test coverage on a credential protection
guard.

## Approach: Test-First (TDD)

Write all failing tests first, documenting every bug. Then fix the code to
make them pass. Tests become permanent regression guards.

## Area 1: Guard Robustness

### New Test File: `pkg/seatbelt/guards/guard_robustness_test.go`

Table-driven tests covering ALL 12 guards uniformly:

```
Guards under test:
  base, system-runtime, network, filesystem, git-integration,
  keychain, node-toolchain, nix-toolchain, project-secrets,
  dev-credentials, aide-secrets, git-remote
```

**Test 1: Nil context safety**

Every guard called with `Rules(nil)` must return empty `GuardResult`
without panicking. Table-driven: iterate all guards from `AllGuards()`,
call `Rules(nil)`, assert no panic and empty or valid result.

Currently failing: aide-secrets, keychain, nix-toolchain,
node-toolchain, system-runtime, dev-credentials, project-secrets,
custom guard (8 of 12).

**Test 2: Empty HomeDir safety**

Every guard called with `&Context{HomeDir: ""}` must return empty
`GuardResult` or rules containing only absolute paths (no relative
paths like `.config/aide`). Table-driven: iterate all guards, call
with empty HomeDir, verify no relative paths in output.

Currently failing: aide-secrets, keychain, node-toolchain,
dev-credentials (paths become relative or wrong).

**Test 3: Symlinked HomeDir**

For guards that generate home-relative paths, when HomeDir is a symlink
(e.g., `/var/folders/tmp/home` -> `/private/var/folders/tmp/home` on
macOS), emitted paths should use the resolved target. Table-driven:
create a symlinked home, run each guard, verify paths use real target.

Applies to: keychain, node-toolchain, aide-secrets, dev-credentials,
git-remote. (git-integration already handles this, nix-toolchain uses
system paths, base/network/filesystem don't use HomeDir for rules.)

### New Test File: `pkg/seatbelt/guards/guard_aide_secrets_test.go`

Dedicated tests for aide-secrets guard (currently zero coverage):

- Metadata (name, type, description)
- Nil context
- Empty HomeDir
- Secrets directory exists: verify deny rules emitted
- Secrets directory missing: verify skipped, no rules
- OptOut via ExtraReadable

### Guard Code Fixes

**Pattern for nil/empty check** — add to start of every affected guard:

```go
func (g *fooGuard) Rules(ctx *seatbelt.Context) seatbelt.GuardResult {
    if ctx == nil || ctx.HomeDir == "" {
        return seatbelt.GuardResult{}
    }
    // ... existing logic ...
}
```

For guards that don't need HomeDir (base, network, project-secrets which
uses ProjectRoot), only add nil check.

**Shared helpers** — move `resolveSymlink` and `expandTilde` from
`gitconfig.go` to `helpers.go` so all guards can use them. Update
gitconfig.go to use the shared versions. Remove `expandHome` from
`guard_custom.go` and replace with the shared `expandTilde` (which
uses string concatenation to preserve trailing slashes, unlike
`expandHome` which used `filepath.Join`).

**Symlink resolution in guards** — each guard that builds paths from
`ctx.HomeDir` calls `resolveSymlink()` on generated paths. Follows the
self-contained principle from c191949.

### Guard File Changes

| File | Change |
|------|--------|
| `guard_robustness_test.go` | New — table-driven tests for all 12 guards |
| `guard_aide_secrets_test.go` | New — dedicated aide-secrets tests |
| `helpers.go` | Add shared `resolveSymlink`, `expandTilde` (move from gitconfig.go) |
| `gitconfig.go` | Remove local resolveSymlink/expandTilde, use shared helpers |
| `guard_aide_secrets.go` | Add nil/empty checks |
| `guard_keychain.go` | Add nil/empty checks |
| `guard_node_toolchain.go` | Add nil/empty checks |
| `guard_nix_toolchain.go` | Add nil check |
| `guard_system_runtime.go` | Add nil check |
| `guard_dev_credentials.go` | Add nil/empty checks |
| `guard_project_secrets.go` | Add nil check |
| `guard_custom.go` | Add nil/empty checks, replace expandHome with shared expandTilde |

Already handled (no changes): guard_git_remote.go, guard_git_integration.go,
guard_filesystem.go, guard_base.go, guard_network.go.

## Area 2: Guard Coverage Gaps

### Nix Guard: System-Level Paths — Verify Before Fixing

The user reported `/etc/nix/registry.json: Operation not permitted`.
However, the system-runtime guard already allows `(subpath "/private")`
for `file-read*`, and on macOS `/etc` is a symlink to `/private/etc`.
This means `/etc/nix/registry.json` SHOULD already be readable.

**Action:** Write a test that verifies `/etc/nix/` paths appear in the
combined sandbox profile. If the system-runtime guard already covers
this, no nix guard changes are needed. If it doesn't (because seatbelt
doesn't follow the `/etc` → `/private/etc` symlink for the subpath
rule), add explicit `/etc/nix` rules to the nix guard.

### Test

Add to `toolchain_test.go`:

```go
func TestGuard_NixToolchain_EtcNixCoverage(t *testing.T) {
    // Verify that /etc/nix/ paths are accessible in the profile
    // Either via system-runtime's /private subpath or nix guard
}
```

### File Changes (conditional)

| File | Change |
|------|--------|
| `toolchain_test.go` | Add `/etc/nix` coverage verification test |
| `guard_nix_toolchain.go` | Add `/etc/nix` read rules ONLY if test proves gap exists |

## Area 3: Auto-Suggest Pipeline Fix

### Problem

`launcher.go:480` gates detection behind zero capabilities:

```go
if len(data.Capabilities) == 0 && len(data.DisabledCaps) == 0 {
    suggestions := capability.DetectProject(projectRoot)
```

Users with partially configured capabilities (the common case) never see
suggestions for missing capabilities.

### Fix

Always run detection. Compare detected capabilities against enabled
capabilities. Show the difference as suggestions in the banner alongside
enabled capabilities:

```
aide · default (claude → /usr/bin/sandbox-exec)
   📁 project override: ~/source/github.com/jskswamy/*
   🛡 sandbox: network outbound only
     ✓ github     ~/.config/gh
     ○ go         ~/go (detected)
     ○ git-remote SSH + network (detected)
   ⚡ AUTO-APPROVE
```

Where `✓` = enabled, `○` = detected but not enabled.

### Implementation

1. **Always run `DetectProject()`** — remove the `len == 0` gate

2. **Filter out already-enabled capabilities** — compute the set
   difference: `detected - enabled = suggestions`

3. **Add `Suggested bool` to `CapabilityDisplay`** — new field to
   distinguish enabled vs suggested in the banner

4. **Add `SuggestedCaps []CapabilityDisplay` to `BannerData`** — separate
   slice from `Capabilities` so templates can render them in a distinct
   block. Keeps enabled and suggested caps cleanly separated.

5. **Update banner templates** — all three template files (`compact.tmpl`,
   `boxed.tmpl`, `clean.tmpl`) need a new range block for
   `.SuggestedCaps`. Show with `○` prefix and "(detected)" suffix.
   The rendering happens in Go templates, NOT in `banner.go`.

### File Changes

| File | Change |
|------|--------|
| `internal/ui/types.go` | Add `Suggested bool` to `CapabilityDisplay`, add `SuggestedCaps` to `BannerData` |
| `internal/launcher/launcher.go` | Always detect, filter enabled, build SuggestedCaps list |
| `internal/ui/templates/compact.tmpl` | Add suggested caps rendering block |
| `internal/ui/templates/boxed.tmpl` | Add suggested caps rendering block |
| `internal/ui/templates/clean.tmpl` | Add suggested caps rendering block |
| `internal/launcher/launcher_test.go` | Test detection with partial caps |

### Tests

- **Launcher test: suggestions with partial caps** — enable `github`,
  detect `github` + `go` + `git-remote`, verify `go` and `git-remote`
  appear as suggestions
- **Launcher test: no suggestions when all detected are enabled** —
  enable all detected caps, verify no suggestions
- **Launcher test: suggestions with zero caps** — same behavior as
  before (backward compatible)
- **Banner rendering test** — verify `○` prefix for suggested caps,
  `✓` for enabled

## Area 4: Testing Strategy

### Execution Order

1. Write `guard_robustness_test.go` — verify tests FAIL (document bugs)
2. Write `guard_aide_secrets_test.go` — verify tests FAIL
3. Fix guard nil/empty/symlink issues one by one
4. Fix nix guard coverage gap
5. Fix auto-suggest pipeline
6. Full test suite passes

### Success Criteria

- `go test ./pkg/seatbelt/guards/ -run TestGuardRobustness` passes
- `go test ./pkg/seatbelt/guards/ -run TestAideSecrets` passes
- `go test ./pkg/seatbelt/guards/ -run TestGuard_NixToolchain_SystemPaths` passes
- `go test ./internal/launcher/ -run TestLauncher_Suggestions` passes
- `go test ./...` passes (no regressions)
- No guard panics on nil context
- No guard produces relative paths on empty HomeDir
- All home-relative paths resolve symlinks
- Banner shows suggested capabilities alongside enabled ones
- Nix eval/flake check work inside sandbox

## Lessons Learned (from hotfixes shipped during this audit)

### Seatbelt evaluates literal paths, not resolved symlinks

**Hotfix v1.4.1:** The system-runtime guard had `(subpath "/private")`
but NOT `(subpath "/etc")`. On macOS, `/etc` → `/private/etc` is a
symlink, but seatbelt checks the LITERAL path `/etc/ssl/...` and finds
no matching rule. This broke ALL HTTPS operations (SSL cert verification
failed) for nix-provided tools where the cert chain traverses
`/etc/ssl/certs/` → `/etc/static/ssl/certs/` → `/nix/store/...`.

**Principle:** When adding seatbelt path rules, always consider BOTH
the literal path AND the symlink target. On macOS, common symlink pairs
include:
- `/etc` → `/private/etc`
- `/var` → `/private/var`
- `/tmp` → `/private/tmp`

The system-runtime guard now covers both `/etc` and `/private` to
handle this. Individual guards should use `resolveSymlink()` on paths
they discover dynamically (gitconfig includes, env overrides, etc.).

### expandTilde must not use filepath.Join

**Hotfix in v1.4.0:** `filepath.Join` strips trailing slashes, breaking
`gitdir:~/work/` patterns that rely on trailing `/` for prefix matching.
The shared `expandTilde` helper uses string concatenation to preserve
the caller's path structure.

### Included configs can override core values

**Hotfix in v1.4.0:** `core.excludesFile` set in an `[includeIf]`
config was ignored because `resolveIncludes` didn't extract core values
from included files. Git uses last-one-wins semantics across all config
files including includes.

## Out of Scope

- Capability pipeline testing — EnableGuard flow, path conflict
  scenarios, capability inheritance edge cases (future Part 2)
- Input validation — SSH_AUTH_SOCK path validation, scanEnvFiles depth
  limits, custom guard path bounds checking (future Part 3)
- Auto-enable capabilities (interactive prompt to enable suggestions) —
  suggestions are shown, user must still use `--with` or config to
  enable. Interactive enablement is a future UX improvement.
