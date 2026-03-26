# Self-Contained Guards: Fix Implicit Cross-Guard Dependencies

**Date:** 2026-03-27
**Status:** Draft
**Type:** Bug fix

## Problem

When aide launches Claude, Claude reports "not logged in" because it
cannot read OAuth tokens from the macOS Keychain. The keychain guard
grants `file-write*` but not `file-read*` to `~/Library/Keychains`,
so token reads are blocked by the deny-default sandbox.

## Root Cause

Two commits created and then exposed a fragile implicit dependency
between guards:

1. **`ab339c5`** ("Add new guards and promote existing guards to
   default") simplified the keychain and nix-toolchain guards by
   removing their `file-read*` rules, adding comments saying "reads
   covered by filesystem guard." This created an implicit cross-guard
   dependency.

2. **`022173b`** ("Narrow filesystem guard to minimal baseline reads")
   correctly narrowed the filesystem guard as part of the capability
   architecture (where each capability like `--with go`, `--with rust`
   explicitly adds its own paths). This broke the implicit dependency,
   leaving these guards with write-only access to paths they need to
   read. After both commits, `~/.nix-profile`, `~/.local/state/nix`,
   `~/Library/Keychains`, and `~/Library/Preferences/com.apple.security.plist`
   have zero read coverage from any guard.

The core issue: commit `ab339c5` broke guard self-containment by
making one guard rely on another for basic path coverage.

## Principle

Each guard must be self-contained for its own paths. No guard should
rely on another guard for basic read/write coverage. This prevents
fragile implicit dependencies that break when other guards are
refactored.

## Path coverage map

Which guard owns which nix-related paths:

| Path | Owner | Access |
|------|-------|--------|
| `/nix/store`, `/nix/var`, `/run/current-system` | system-runtime | read (no change) |
| `~/.nix-profile`, `~/.local/state/nix`, `~/.cache/nix` | nix-toolchain | read+write (fix) |
| `~/.nix-defexpr`, `~/.config/nix` | nix-toolchain | read (restore) |
| `~/.cache` (broad, includes `~/.cache/nix`) | filesystem | read+write (no change) |

Note: `~/.cache/nix` has overlapping coverage from both the filesystem
guard (via broad `~/.cache`) and the nix-toolchain guard. This
duplication is intentional — the nix guard must be self-contained even
if another guard happens to cover a subset of its paths.

## Design

### 1. Keychain guard (`pkg/seatbelt/guards/guard_keychain.go`)

Change `file-write*` to `file-read*` for user keychain paths. Agents
read tokens from the keychain to authenticate. They should not write
to the user's keychain — the macOS Security framework handles token
storage through its Mach services (already allowed by this guard), not
through direct file writes. Removing `file-write*` is a security
tightening, not a regression.

Remove the stale comment on lines 26-27 ("reads covered by filesystem
guard's ~/Library/Keychains allow"). Keep the comment on lines 34-35
about system-runtime covering system keychain reads — that dependency
is still valid.

**Before:**
```go
// User keychain (read-write) — reads covered by filesystem guard's
// ~/Library/Keychains allow, but writes need explicit allow
seatbelt.SectionAllow("User keychain (write)"),
seatbelt.AllowRule(`(allow file-write*
    ` + seatbelt.HomeSubpath(home, "Library/Keychains") + `
    ` + seatbelt.HomeLiteral(home, "Library/Preferences/com.apple.security.plist") + `
)`),
```

**After:**
```go
seatbelt.SectionAllow("User keychain (read-only)"),
seatbelt.AllowRule(fmt.Sprintf(`(allow file-read*
    %s
    %s
)`, seatbelt.HomeSubpath(home, "Library/Keychains"),
   seatbelt.HomeLiteral(home, "Library/Preferences/com.apple.security.plist"))),
```

### 2. Nix-toolchain guard (`pkg/seatbelt/guards/guard_nix_toolchain.go`)

Change `file-write*` to `file-read* file-write*` for nix user paths
(nix operations genuinely need both read and write). Restore the
`~/.nix-defexpr` and `~/.config/nix` read rules that were completely
dropped in `ab339c5` — these are HOME paths not covered by
system-runtime.

Remove the stale comment on line 34 ("reads covered by filesystem
guard").

**Before:**
```go
// Nix user paths (write only — reads covered by filesystem guard)
seatbelt.SectionAllow("Nix user paths (write)"),
seatbelt.AllowRule(`(allow file-write*
    ` + seatbelt.HomeSubpath(home, ".nix-profile") + `
    ` + seatbelt.HomeSubpath(home, ".local/state/nix") + `
    ` + seatbelt.HomeSubpath(home, ".cache/nix") + `
)`),
```

**After:**
```go
seatbelt.SectionAllow("Nix user paths"),
seatbelt.AllowRule(fmt.Sprintf(`(allow file-read* file-write*
    %s
    %s
    %s
)`, seatbelt.HomeSubpath(home, ".nix-profile"),
   seatbelt.HomeSubpath(home, ".local/state/nix"),
   seatbelt.HomeSubpath(home, ".cache/nix"))),

seatbelt.SectionAllow("Nix channel definitions and user config"),
seatbelt.AllowRule(fmt.Sprintf(`(allow file-read*
    %s
    %s
)`, seatbelt.HomeSubpath(home, ".nix-defexpr"),
   seatbelt.HomeSubpath(home, ".config/nix"))),
```

### 3. Update tests (`pkg/seatbelt/guards/toolchain_test.go`)

**Keychain test** (`TestGuard_Keychain_Rules`, line 146):
- Line 152: Change comment from "write paths" to "read paths"
- Line 153: Assertion stays (path is still present), but verify
  `file-read*` is in output and `file-write*` is NOT (for the
  keychain section)

**Nix-toolchain test** (`TestGuard_NixToolchain_Rules`, line 80):
- Line 100: Remove comment about reads being covered elsewhere
- Lines 112-115: Reverse the negative assertion — `file-read*` MUST
  be present (was: must be absent)
- Lines 118-129: Remove `~/.nix-defexpr` and `~/.config/nix` from the
  "should NOT contain" list. Add positive assertions that these paths
  ARE present with `file-read*`.
- Keep negative assertions for `/nix/store`, `/nix/var`,
  `/run/current-system` — those are still owned by system-runtime.

**Filesystem guard tests**: No changes. `TestFilesystemGuard_NarrowBaseline`
already correctly asserts `Library/Keychains` is NOT in the filesystem
guard.

## What doesn't change

- The filesystem guard's narrow baseline is correct and stays as-is.
- The capability architecture (`--with go`, `--with rust`, etc.) is
  correct.
- The dev-credentials guard's stale comment about "allowed
  directories" is cosmetic — the behavior is correct (denies are
  no-ops when parent is not readable, active when capability enables
  the parent).
- System-runtime guard continues to cover `/nix/store`, `/nix/var`,
  `/run`, `/Library` system paths.
- The `renderTestRules` helper needs no changes.

## Testing

Run the guard tests to verify:

```
nix develop --command go test ./pkg/seatbelt/guards/ -v
```
