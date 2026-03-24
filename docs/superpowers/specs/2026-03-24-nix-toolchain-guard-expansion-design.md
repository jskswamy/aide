# macOS Sandbox Fixes: Nix Guard, writable_extra, Dead Config, Verification Gap

**Date:** 2026-03-24
**Status:** Approved
**Scope:** `pkg/seatbelt/guards/`, `internal/sandbox/`

## Problem

The `nix-toolchain` guard is too narrow for a functional nix-darwin + home-manager
development environment inside the aide sandbox. Three categories of failure:

1. **Path traversal metadata** — `filepath.EvalSymlinks` walks path components
   doing `lstat` on each. Rules like `(subpath "/nix/store")` allow children but
   not `/nix` itself. Any Go binary (or any code calling `realpath`) that
   traverses through `/nix` or `/run` fails with "operation not permitted".

2. **`/run` firmlink** — On macOS, `/run` is a synthetic firmlink to
   `/private/var/run`. Seatbelt cannot resolve it, so
   `(subpath "/run/current-system")` never matches. The rule must target
   `/private/var/run/current-system` instead.

3. **Missing paths** — Nix daemon socket (Unix socket connect), channel
   definitions (`~/.nix-defexpr/`), and user config (`~/.config/nix/`) are not
   covered by the current guard.

### Confirmed Root Cause

Tested inside the aide sandbox:

```
$ stat /nix
stat: cannot stat '/nix': Operation not permitted

$ stat /run/current-system
stat: cannot stat '/run/current-system': Operation not permitted

$ filepath.EvalSymlinks("/Users/subramk/.nix-profile/bin/go")
→ lstat /nix: operation not permitted

$ /Users/subramk/.nix-profile/bin/go env GOROOT
→ go: cannot find GOROOT directory: 'go' binary is trimmed and GOROOT is not set
```

All work correctly outside the sandbox.

## Design

All changes are in the single file `pkg/seatbelt/guards/guard_nix_toolchain.go`.

**Note:** `Rules()` is called at profile *generation* time (outside the sandbox),
so filesystem checks like `dirExists` have full access. The generated profile is
then applied to the sandboxed child process.

### 1. Detection Gate

Skip all nix rules when nix is not installed:

```go
func (g *nixToolchainGuard) Rules(ctx *seatbelt.Context) seatbelt.GuardResult {
    if !dirExists("/nix/store") {
        return seatbelt.GuardResult{
            Skipped: []string{"/nix/store not found — nix not installed"},
        }
    }
    // ... emit rules
}
```

Follows the existing pattern from other guards (e.g., ssh-keys). The guard
remains `Type() = "always"` because on nix systems every sandboxed process needs
these rules — but the detection gate makes it a no-op on non-nix systems.

### 2. Path Traversal Metadata

Add `file-read-metadata` for parent directories needed by `lstat`/`readlink`
during symlink resolution:

```seatbelt
(allow file-read-metadata
    (literal "/nix")
    (literal "/run")
)
```

### 3. Fix `/run/current-system` Resolution

Replace the broken firmlink path with the real target. Keep the original for
portability across macOS versions:

```seatbelt
(allow file-read*
    (subpath "/nix/store")
    (subpath "/nix/var")
    (subpath "/run/current-system")
    (subpath "/private/var/run/current-system")
)
```

### 4. Nix Daemon Socket

Allow Unix socket connection to the nix daemon. Uses `(remote unix-socket ...)`
consistent with the existing network guard pattern (`(remote tcp ...)`,
`(remote udp ...)`):

```seatbelt
(allow network-outbound
    (remote unix-socket (path-literal "/nix/var/nix/daemon-socket/socket"))
)
```

Required by all `nix` commands (`nix develop`, `nix build`, `nix-shell`, etc.).

**Note:** Read-only access to `/nix/var` is sufficient for the sandboxed process.
The daemon itself handles all writes to `/nix/var` (builds, gc, db updates) —
clients only communicate via the socket.

### 5. User Paths

Add read access for channel definitions and user config:

```seatbelt
(allow file-read*
    <HomeSubpath "~/.nix-defexpr">
    <HomeSubpath "~/.config/nix">
)
```

### Design Notes

- The existing `(subpath "/nix/var")` rule grants `file-read*` which includes
  `file-read-metadata`. This implicitly covers metadata needed for profile
  symlink resolution through `/nix/var/nix/profiles/`. No additional metadata
  rules needed for that chain.
- `/private` and `/private/var` metadata access is already covered by the
  system-runtime guard (`(subpath "/private/var/db/timezone")` etc.).
  `/private/var/run` is accessible (confirmed by `stat` inside sandbox).
- System-wide nix config at `/private/etc/nix/` is covered by the system-runtime
  guard's existing `/private/etc` rules. User-level `~/.config/nix/` is not.
- `~/.nix-channels` is omitted — nix-darwin + home-manager uses flakes, and
  channel config is covered by `~/.nix-defexpr/`.

---

## Problem 2: `writable_extra` / `readable_extra` Silently Ignored

### Confirmed Root Cause

`config.SandboxPolicy` parses `writable_extra` and `readable_extra` from YAML.
`PolicyFromConfig` validates them and warns on conflicts. But **neither field is
ever read** from config or wired into the `Policy` struct. The `Policy` struct has
`ExtraDenied` but no `ExtraWritable` or `ExtraReadable`.

The filesystem guard only reads `ctx.ProjectRoot`, `ctx.HomeDir`, `ctx.RuntimeDir`,
`ctx.TempDir`, and `ctx.ExtraDenied`. User-specified writable/readable paths from
config are silently dropped.

**Impact:** Users who configure `writable_extra: [~/.config/gcloud]` get no error
but gcloud access is still blocked. The workaround is copying config to `/tmp`.

### Design

#### 6. Wire `writable_extra` / `readable_extra` Through to Profile

**a. Add fields to `Policy` struct** (`internal/sandbox/sandbox.go`):

```go
type Policy struct {
    // ... existing fields ...
    ExtraWritable []string  // User-configured extra writable paths
    ExtraReadable []string  // User-configured extra readable paths
    ExtraDenied   []string  // (already exists)
}
```

**b. Read from config in `PolicyFromConfig`** (`internal/sandbox/policy.go`):

Follow the same pattern as `ExtraDenied` — resolve templates, validate paths,
assign to policy:

```go
// writable_extra
if len(cfg.WritableExtra) > 0 {
    extra, err := ResolvePaths(cfg.WritableExtra, templateVars)
    if err != nil { return nil, nil, err }
    policy.ExtraWritable = validateAndFilterPaths(extra, &warnings)
} else if len(cfg.Writable) > 0 {
    w, err := ResolvePaths(cfg.Writable, templateVars)
    if err != nil { return nil, nil, err }
    policy.ExtraWritable = validateAndFilterPaths(w, &warnings)
}

// Same for readable_extra / readable
```

**c. Add to `seatbelt.Context`** (`pkg/seatbelt/module.go`):

```go
type Context struct {
    // ... existing fields ...
    ExtraWritable []string
    ExtraReadable []string
}
```

**d. Pass through in `generateSeatbeltProfile`** (`internal/sandbox/darwin.go`):

```go
c.ExtraWritable = policy.ExtraWritable
c.ExtraReadable = policy.ExtraReadable
```

**e. Consume in filesystem guard** (`pkg/seatbelt/guards/guard_filesystem.go`):

```go
func (g *filesystemGuard) Rules(ctx *seatbelt.Context) seatbelt.GuardResult {
    var writable, readable []string
    // ... existing ProjectRoot, HomeDir, RuntimeDir, TempDir logic ...

    writable = append(writable, ctx.ExtraWritable...)
    readable = append(readable, ctx.ExtraReadable...)

    return seatbelt.GuardResult{Rules: filesystemRules(writable, readable, ctx.ExtraDenied)}
}
```

---

## Problem 3: Architectural Gap — No Semantic Verification

### Why These Bugs Went Undetected

All three bugs share the same root cause: **the test suite validates syntax
(string output) but never verifies semantics (actual sandboxed behavior)**.

| Bug | What tests check | What tests miss |
|-----|-----------------|-----------------|
| `/nix` metadata | "does `/nix/store` appear?" | "can a process `lstat /nix`?" |
| `/run` firmlink | "does `/run/current-system` appear?" | "can a process read `/run/current-system`?" |
| `writable_extra` | "does config parse?" | "does the parsed value produce a rule?" |

The test pyramid today:

```
Unit tests (present):     Guard emits expected strings
Integration tests (weak): Basic read/write/deny on temp dirs
Config→Profile (absent):  Config field X produces rule Y in profile
Profile→Runtime (absent): Generated profile allows operation Z
```

### Design: Config-to-Profile Contract Tests

Add a new test category that verifies **config fields produce expected rules**
in the rendered profile. This catches the `writable_extra` class of bugs — where
a config field is parsed but never reaches the profile.

**File:** `internal/sandbox/policy_contract_test.go`

```go
func TestPolicyContract_WritableExtraProducesRule(t *testing.T) {
    cfg := &config.SandboxPolicy{
        WritableExtra: []string{"/custom/path"},
    }
    policy, _, err := PolicyFromConfig(cfg, "/project", "/runtime", "/home", "/tmp")
    require.NoError(t, err)

    sb := &darwinSandbox{}
    profile, err := sb.GenerateProfile(*policy)
    require.NoError(t, err)

    assert.Contains(t, profile, "/custom/path")
    assert.Contains(t, profile, "file-write*")
}
```

**Principle:** For every config field that should affect the sandbox profile,
there must be a test that:
1. Sets the config field
2. Renders the full profile
3. Asserts the expected rule appears in the output

This is cheap to write and catches the entire class of "parsed but dropped" bugs.

### Design: Toolchain Smoke Tests

Add integration tests (behind `//go:build integration`) that verify toolchain
guards work against real filesystem operations:

**File:** `internal/sandbox/toolchain_integration_test.go`

```go
func TestNixGuard_SymlinkResolution(t *testing.T) {
    if !dirExists("/nix/store") {
        t.Skip("nix not installed")
    }
    // Generate profile with default policy
    // Run: sandbox-exec -f profile.sb /usr/bin/stat /nix
    // Assert: exit code 0
}

func TestNixGuard_GoToolchain(t *testing.T) {
    if !dirExists("/nix/store") {
        t.Skip("nix not installed")
    }
    // Generate profile with default policy
    // Run: sandbox-exec -f profile.sb <nix go binary> env GOROOT
    // Assert: output contains /nix/store, exit code 0
}
```

These are environment-specific (only run on nix systems) but they catch the
exact class of bugs we hit — rules that look correct but fail at runtime.

---

## Problem 4: `allow_subprocess` Dead Code on Darwin

### Root Cause

`Policy.AllowSubprocess` is set from config (`policy.go:88-90`) but never passed
to `seatbelt.Context`. The `Context` struct has no `AllowSubprocess` field. The
system-runtime guard unconditionally emits `(allow process-exec)` and
`(allow process-fork)` (`guard_system_runtime.go:111-112`).

Setting `allow_subprocess: false` in config has zero effect on macOS.

### Design

#### 7. Wire `AllowSubprocess` to System Runtime Guard

**a. Add to `seatbelt.Context`** (`pkg/seatbelt/module.go`):

```go
type Context struct {
    // ... existing fields ...
    AllowSubprocess bool
}
```

**b. Pass through in `generateSeatbeltProfile`** (`internal/sandbox/darwin.go`):

```go
c.AllowSubprocess = policy.AllowSubprocess
```

**c. Make process rules conditional** (`guard_system_runtime.go`):

```go
// Process rules — conditional on AllowSubprocess
if ctx.AllowSubprocess {
    rules = append(rules,
        seatbelt.AllowRule("(allow process-exec)"),
        seatbelt.AllowRule("(allow process-fork)"),
    )
} else {
    // Allow exec of the agent binary itself but deny forking children
    rules = append(rules,
        seatbelt.AllowRule("(allow process-exec)"),
        seatbelt.AllowRule("(deny process-fork)"),
    )
}
```

**Note:** We still need `process-exec` even when subprocess is disabled — the
agent binary itself must execute. The restriction is on `process-fork` which
prevents spawning child processes. However, this is a nuanced area — many agents
rely on forking (e.g., git, shell commands). For now, we wire the field through
and default to `true`. The deny behavior can be refined later.

---

## Problem 5: Broken Darwin Integration Tests

### Root Cause

`internal/sandbox/integration_test.go` constructs `Policy` structs with fields
that don't exist: `Denied`, `Writable`, `Readable`. The current `Policy` struct
uses `ExtraDenied`, `ProjectRoot`, `RuntimeDir`, etc. These tests only compile
under `//go:build darwin && integration`, so the breakage went unnoticed.

### Design

#### 8. Rewrite Integration Tests to Use Guard-Based Policy

Rewrite the four tests in `integration_test.go` to use `DefaultPolicy` and
guard-based configuration instead of raw path lists:

```go
func TestSandbox_DeniedPathBlocked(t *testing.T) {
    runtimeDir := realPath(t, t.TempDir())
    deniedDir := realPath(t, t.TempDir())
    secretFile := filepath.Join(deniedDir, "id_rsa")
    os.WriteFile(secretFile, []byte("TOP SECRET"), 0600)

    policy := DefaultPolicy(deniedDir, runtimeDir, os.TempDir(), os.Environ())
    policy.ExtraDenied = []string{secretFile}

    cmd := exec.Command("/bin/cat", secretFile)
    cmd.Env = os.Environ()
    // ... apply and assert blocked
}
```

---

## Problem 6: Parent-Metadata Traversal Helper

### Root Cause

The nix guard's `/nix` metadata bug is a systemic pattern. Any guard using
`(subpath "/some/path")` needs `file-read-metadata (literal "/some")` for the
parent. Without a helper, every new toolchain guard will repeat this bug.

### Design

#### 9. Add `SubpathWithParentMetadata` Helper

**File:** `pkg/seatbelt/path.go`

```go
// SubpathWithParentMetadata returns rules for a subpath AND file-read-metadata
// on its parent directory. This ensures lstat/readlink works during symlink
// resolution — seatbelt subpath rules don't cover the parent itself.
func SubpathWithParentMetadata(path string) []Rule {
    parent := filepath.Dir(path)
    return []Rule{
        AllowRule(fmt.Sprintf(`(allow file-read* (subpath %q))`, path)),
        AllowRule(fmt.Sprintf(`(allow file-read-metadata (literal %q))`, parent)),
    }
}
```

Guards should use this helper instead of raw `(subpath ...)` for paths where
the parent directory is not already covered by another rule. The nix guard
should be refactored to use it.

**Note:** Not all subpaths need this — `/nix/store` needs it because `/nix`
isn't covered elsewhere, but `/usr/local/bin` doesn't because `/usr` is already
allowed by system-runtime. The helper is opt-in, not mandatory.

---

## Problem 7: Cross-Guard Conflict Detection (Diagnostic Only)

### Root Cause

The npm guard (opt-in) emits `(deny file-read-data ~/.npmrc)` which silently
overrides the node-toolchain guard's (always) `(allow file-read* ~/.npmrc)`.
Seatbelt's deny-always-wins means enabling npm guard breaks npm config reading.
There's no warning.

### Design

#### 10. Guard Conflict Diagnostic in `EvaluateGuards`

Add a post-evaluation scan in `EvaluateGuards` (`internal/sandbox/sandbox.go`)
that detects when a deny rule from one guard covers a path that another guard
explicitly allows:

```go
func detectConflicts(results []seatbelt.GuardResult) []string {
    // Collect all denied paths (from deny rules)
    // Collect all allowed paths (from allow rules)
    // For each denied path, check if any other guard allows it
    // Return warnings like:
    //   "guard 'npm' denies ~/.npmrc which guard 'node-toolchain' allows"
}
```

This is **diagnostic only** — it produces warnings for the banner, not errors.
The deny-wins behavior is correct by design; the diagnostic helps users
understand why something is blocked.

## Changes Summary

| File | Change |
|------|--------|
| **Problem 1: Nix guard** | |
| `pkg/seatbelt/guards/guard_nix_toolchain.go` | Expand rules (sections 1–5) |
| `pkg/seatbelt/guards/toolchain_test.go` | Update nix guard tests |
| **Problem 2: writable_extra** | |
| `internal/sandbox/sandbox.go` | Add `ExtraWritable`, `ExtraReadable` to `Policy` |
| `pkg/seatbelt/module.go` | Add `ExtraWritable`, `ExtraReadable` to `Context` |
| `internal/sandbox/policy.go` | Wire `writable_extra`/`readable_extra` in `PolicyFromConfig` |
| `internal/sandbox/darwin.go` | Pass extra paths through to context |
| `pkg/seatbelt/guards/guard_filesystem.go` | Consume `ExtraWritable`/`ExtraReadable` |
| **Problem 3: Verification gap** | |
| `internal/sandbox/policy_contract_test.go` | New: config-to-profile contract tests |
| `internal/sandbox/toolchain_integration_test.go` | New: toolchain smoke tests |
| **Problem 4: allow_subprocess** | |
| `pkg/seatbelt/module.go` | Add `AllowSubprocess` to `Context` |
| `internal/sandbox/darwin.go` | Pass `AllowSubprocess` to context |
| `pkg/seatbelt/guards/guard_system_runtime.go` | Conditional process-fork |
| **Problem 5: Broken integration tests** | |
| `internal/sandbox/integration_test.go` | Rewrite to use guard-based `Policy` |
| **Problem 6: Parent-metadata helper** | |
| `pkg/seatbelt/path.go` | Add `SubpathWithParentMetadata` helper |
| **Problem 7: Cross-guard conflicts** | |
| `internal/sandbox/sandbox.go` | Add conflict detection in `EvaluateGuards` |

## Testing

**Unit tests** (in `toolchain_test.go`):
- Detection gate: when `/nix/store` does not exist, `Rules()` returns
  `Skipped` with no rules
- `file-read-metadata` rules for `/nix` and `/run` appear in output
- `/private/var/run/current-system` subpath appears alongside `/run/current-system`
- Unix socket rule for nix daemon appears with `(remote unix-socket ...)` syntax
- `HomeSubpath` entries for `~/.nix-defexpr` and `~/.config/nix` appear

**Contract tests** (in `policy_contract_test.go`):
- `writable_extra` config field produces `file-write*` rule in profile
- `readable_extra` config field produces `file-read*` rule in profile
- `denied` / `denied_extra` produce deny rules (already covered, add for parity)
- `allow_subprocess: false` produces `deny process-fork` in profile
- `allow_subprocess: true` (default) produces `allow process-fork`
- Each `ExtraDenied` field produces both `file-read-data` and `file-write*` deny

**Integration** (in `integration_test.go`, rewritten):
- Denied path blocked (using `ExtraDenied` on guard-based Policy)
- Allowed path readable (using default guards)
- Writable path works (using `ExtraWritable`)
- Write to read-only blocked

**Toolchain integration** (in `toolchain_integration_test.go`, `//go:build integration`):
- Nix symlink resolution: `stat /nix` succeeds inside sandbox
- Go toolchain: nix-installed `go env GOROOT` succeeds inside sandbox

**Guard conflict diagnostic:**
- Test that enabling npm guard produces warning about `.npmrc` conflict
  with node-toolchain guard

**Manual**: `go test ./...` and `nix develop` work inside aide sandbox
