# Nix Toolchain Guard Expansion

**Date:** 2026-03-24
**Status:** Approved
**Scope:** `pkg/seatbelt/guards/guard_nix_toolchain.go`

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

Follows the existing pattern from other guards (e.g., ssh-keys).

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

Allow Unix socket connection to the nix daemon:

```seatbelt
(allow network-outbound
    (to unix-socket (path-literal "/nix/var/nix/daemon-socket/socket"))
)
```

Required by all `nix` commands (`nix develop`, `nix build`, `nix-shell`, etc.).

### 5. User Paths

Add read access for channel definitions and user config:

```seatbelt
(allow file-read*
    <HomeSubpath "~/.nix-defexpr">
    <HomeSubpath "~/.config/nix">
)
```

## Changes Summary

| File | Change |
|------|--------|
| `pkg/seatbelt/guards/guard_nix_toolchain.go` | Expand rules (sections 1–5) |
| `pkg/seatbelt/guards/toolchain_test.go` | Update nix guard tests |

## Testing

- Existing tests updated to match new rule output
- Integration test: `go test ./...` succeeds inside aide sandbox (currently fails)
- Manual: `nix develop` works inside aide sandbox
