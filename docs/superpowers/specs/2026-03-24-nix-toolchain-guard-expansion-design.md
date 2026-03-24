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

## Changes Summary

| File | Change |
|------|--------|
| `pkg/seatbelt/guards/guard_nix_toolchain.go` | Expand rules (sections 1–5) |
| `pkg/seatbelt/guards/toolchain_test.go` | Update nix guard tests |

## Testing

**Unit tests** (in `toolchain_test.go`):
- Detection gate: when `/nix/store` does not exist, `Rules()` returns
  `Skipped` with no rules
- `file-read-metadata` rules for `/nix` and `/run` appear in output
- `/private/var/run/current-system` subpath appears alongside `/run/current-system`
- Unix socket rule for nix daemon appears with `(remote unix-socket ...)` syntax
- `HomeSubpath` entries for `~/.nix-defexpr` and `~/.config/nix` appear

**Integration**: `go test ./...` succeeds inside aide sandbox (currently fails)

**Manual**: `nix develop` works inside aide sandbox
