# Sandbox DefaultPolicy: Switch to (allow default) + Deny-List

**Date:** 2026-03-20
**Status:** Draft
**Scope:** `darwin.go` Seatbelt profile + `DefaultPolicy` in `sandbox.go`

## Problem

Three interacting issues prevent the macOS sandbox from working:

### 1. `(deny default)` breaks Claude's TUI

Extensive testing (documented in `docs/sandbox-findings.md`) proved that `(deny default)` with selective allows cannot support Claude Code's interactive mode:

- `claude --version` works with extensive operation enumeration
- `claude -p "prompt"` works with global read + restricted write
- **Interactive TUI hangs silently** in all configurations using `(deny default)`

Root cause: `sandbox-exec` is deprecated (since macOS 10.15) and Claude Code's TUI (ink/React) requires undocumented Seatbelt operations. The failures produce no log entries, making them impossible to debug.

### 2. Seatbelt deny rules are defeated by operation mismatch

The current `darwin.go` uses `(deny file-read* ...)` but the global allow uses `(allow file-read-data ...)`. Seatbelt gives precedence to specific operations over wildcards, so the deny rules are **always defeated**.

### 3. DefaultPolicy whitelists too few readable paths

The whitelist of specific dirs under `$HOME` breaks on Nix, Homebrew, npm, etc. The `extraReadablePaths()` function grew to 25 entries and still didn't work.

### Evidence

From `docs/sandbox-findings.md` (tested on this machine):

| Profile | `--version` | `-p "prompt"` | Interactive TUI |
|---------|------------|---------------|-----------------|
| `(deny default)` + allow-list | Works | Hangs | Hangs |
| `(deny default)` + global read | Works | Works | **Hangs** |
| `(allow default)` + deny writes | Works | Works | **Works** |

Seatbelt rule matching (tested with `sandbox-exec`):
- `(allow file-read-data ...)` + `(deny file-read* ...)` → **ALLOWED** (specific beats wildcard)
- `(allow file-read-data ...)` + `(deny file-read-data ...)` → ordering-dependent (last wins)

## Solution

Switch the Seatbelt profile from `(deny default)` to `(allow default)`:

```scheme
(version 1)
(allow default)

;; Deny reading secrets (last-match-wins, so these override allow default)
(deny file-read-data (literal "~/.ssh/id_rsa"))
(deny file-read-data (literal "~/.aws/credentials"))
;; ... other sensitive paths

;; Deny writing outside approved paths
(deny file-write*
    (require-not
        (require-any
            (subpath "$PROJECT_ROOT")
            (subpath "$TMPDIR")
            (subpath "$RUNTIME_DIR")
            (subpath "~/.claude")
            (subpath "~/.config/claude")
            (subpath "~/Library/Application Support/Claude")
            (literal "/dev/null")
            (literal "/dev/tty")
        )
    )
)

;; Network (when mode is "none")
(deny network*)
```

**Security properties preserved:**
1. Agents cannot write outside approved directories
2. Agents cannot read SSH keys, cloud credentials, or browser profiles
3. Network can be fully restricted (for offline mode)

**What we give up:** Blocking unknown/unexpected Seatbelt operations (mach ports, IOKit, XPC, etc.). In practice, `deny default` was unmaintainable — each macOS version introduces new required operations.

## Changes

### `internal/sandbox/darwin.go`

**`generateSeatbeltProfile()`** — Complete rewrite of the profile generation:

1. Replace `(deny default)` with `(allow default)`
2. Remove all `(allow ...)` rules for read operations, process, IPC, etc. — they're covered by `allow default`
3. Add `(deny file-write* (require-not (require-any ...)))` with writable paths as exceptions
4. Keep `(deny file-read-data ...)` for denied paths (secrets) — these override `allow default` because `allow default` is less specific than a path-qualified deny
5. Network rules: For `NetworkNone`, add `(deny network*)`. For `NetworkOutbound`, add `(deny network-inbound)`. For `NetworkUnrestricted`, no network deny. Port-level deny rules stay as-is.

**Key Seatbelt semantics with `(allow default)`:**
- `(allow default)` is an implicit allow for everything
- `(deny file-read-data (literal "..."))` overrides it because explicit deny + path filter is more specific than blanket allow
- `(deny file-write* (require-not ...))` blocks all writes except the listed exceptions

### `internal/sandbox/sandbox.go`

**`DefaultPolicy()`** — Simplify `Readable` to just `homeDir` (+ `projectRoot` for Linux). Delete `extraReadablePaths()`.

```go
Readable: []string{
    homeDir,
    projectRoot,
},
```

**Delete `extraReadablePaths()`** — No longer needed.

**Retain `extraWritablePaths()`** — Still needed for write access to agent config dirs.

### Tests

- Update `TestDefaultPolicy_Paths` — expect `homeDir` in `Readable`, remove `.gitconfig`/`.ssh/known_hosts` assertions
- Rewrite `TestGenerateSeatbeltProfile_DenyDefault` — assert `(allow default)` instead of `(deny default)`
- Rewrite `TestGenerateSeatbeltProfile_DeniedAfterAllows` — assert `(deny file-read-data ...)` appears in profile
- Add `TestGenerateSeatbeltProfile_WriteRestriction` — assert `(deny file-write* (require-not ...))` with writable path exceptions
- Update `TestGenerateSeatbeltProfile_ReadGlobal` — assert profile does NOT contain `(allow file-read-data (subpath "/"))` (no longer needed with `allow default`)
- Fix `TestGenerateSeatbeltProfile_DeniedPaths` — change `file-read*` assertion to `file-read-data`

### Known limitation: glob-based deny

The denied list uses glob patterns (`~/.ssh/id_*`) expanded by `filepath.Glob()` at profile-generation time. Only files that exist when the profile is generated are denied. Accepted as known limitation.

### No changes to

- `policy.go` — `PolicyFromConfig` merge logic is unaffected
- `launcher.go` — Already applies sandbox by default via `ResolveSandboxRef`

## Platform Notes

### macOS (Seatbelt)
`(allow default)` + targeted denies. TUI works. Write protection enforced.

### Linux (Landlock/bwrap)
Landlock is allowlist-only. Linux sandbox continues to use existing behavior unchanged. Out of scope.

## Verification Gate

All must pass before commit:

```bash
# 1. Unit tests
go test ./...

# 2. Generate the Seatbelt profile
go run ./cmd/aide sandbox test > /tmp/aide-sandbox.sb

# 3. Agent binary works (non-interactive)
sandbox-exec -f /tmp/aide-sandbox.sb $(which claude) --version
# Expected: prints version

# 4. Agent works with prompt (non-interactive)
sandbox-exec -f /tmp/aide-sandbox.sb $(which claude) -p "say hello"
# Expected: prints response

# 5. Sensitive file denied
sandbox-exec -f /tmp/aide-sandbox.sb cat ~/.ssh/id_ed25519
# Expected: "Operation not permitted"

# 6. Write outside approved dirs denied
sandbox-exec -f /tmp/aide-sandbox.sb touch /tmp/aide-write-test-outside.txt
# Expected: "Operation not permitted" (unless /tmp is in writable list)
```

## What This Does NOT Cover

- Linux Landlock deny-list support — requires separate design
- Interactive TUI verification — requires manual testing (can't easily automate TUI testing)
- Sandbox CLI commands — already implemented in other commits
- Network port filtering — already implemented
