# Sandbox Implementation Findings

## Problem Statement

Running `aide` with a default-on sandbox causes either:
1. **SIGABRT** — Seatbelt profile too restrictive, process killed during startup
2. **Silent hang** — Claude starts but TUI never renders, process unresponsive to Ctrl+C

The previous fix (commit `fe3de2c`) reverted sandbox to opt-in only, leaving users unprotected by default.

## What We Tried

### Attempt 1: Enumerate All Required Paths (deny-default, allow-list)

Started with `(deny default)` and tried to enumerate every path claude needs.

**Missing OS operations discovered iteratively:**

| Operation | Why needed | Symptom when blocked |
|-----------|-----------|---------------------|
| `file-read-metadata` | dyld stat() during path resolution | SIGABRT at startup |
| `file-read-xattr` | Code signing checks | SIGABRT at startup |
| `file-ioctl` | Terminal I/O (isatty, tcgetattr) | SIGABRT at startup |
| `file-read-data (literal "/")` | dyld reads root directory entry | SIGABRT at startup |
| `signal (target self)` | Process self-signaling | SIGABRT at startup |
| `pseudo-tty` | Node.js PTY operations | Silent hang |
| `ipc-posix-shm*` | Node.js shared memory | Silent hang |
| `user-preference-read` | macOS user defaults (keychain via `security` cmd) | Silent hang |

**Missing paths discovered via `log show --predicate "eventMessage CONTAINS 'deny'":**

| Path | Why needed |
|------|-----------|
| `/System/Volumes/Preboot/Cryptexes/OS` | macOS cryptex filesystem (system libs) |
| `/dev/dtracehelper` | dtrace helper device (read + write) |
| `/Library/Preferences/Logging/...` | Logging preferences |
| `/private/var/db/timezone/tz/...` | Timezone data — Node.js hangs without this |
| `/private/var/db/mds/messages/...` | Security messages database |
| `/dev/ttys*`, `/dev/pty*` | Terminal devices — need both read and write |
| `~/Library/Keychains/` | Keychain database access |

**Result:** After fixing all the above, `claude --version` and `claude --help` worked. But `claude -p "prompt"` and interactive mode still hung silently — no sandbox denial logs, meaning something deeper was blocked.

### Attempt 2: Global Read + Restricted Write (allow-default reads)

Switched to `(allow file-read-data (subpath "/"))` — allow reading everything globally, restrict only writes.

```scheme
(allow file-read-data (subpath "/"))  ;; read anything
(deny file-read* (literal "~/.ssh/id_*"))  ;; except secrets
(allow file-write* (subpath "~/.claude"))  ;; write only to allowed dirs
```

**Result:** `claude -p "say hi"` returned "Hi!" — non-interactive mode worked. But the TUI (interactive mode) still didn't render. Claude process ran but produced no visible output.

### Root Cause Analysis

The fundamental issue: **`sandbox-exec` on macOS is deprecated** (since macOS 10.15) and **not designed for TUI applications**.

1. **`(deny default)` is too aggressive** — macOS has hundreds of Seatbelt operations (mach ports, IOKit, XPC, audit, etc.) and there's no documentation of which ones a Node.js TUI app needs. Each macOS version can add new required operations.

2. **Whack-a-mole problem** — Every time we fix one denial, another surfaces. The denials cascade: fixing file reads reveals IPC blocks, fixing IPC reveals preference blocks, fixing preferences reveals XPC blocks, etc.

3. **TUI rendering requires undocumented operations** — Claude Code uses ink (React for CLI) which needs terminal capabilities that go beyond simple read/write. The exact Seatbelt operations for full terminal control aren't documented.

4. **No sandbox denial logs for the final hang** — The most insidious failures produce no log entries, making debugging impossible without `dtrace` (which itself requires SIP disabled).

## Debugging Methodology

### Generating Seatbelt Profiles

```bash
# Build aide and use sandbox show to see effective policy
aide sandbox show

# Use GenerateProfile to get the actual .sb file
go run ./cmd/dump-profile/  # writes to /tmp/aide-debug-rt/sandbox.sb
```

### Testing Manually

```bash
# Test basic execution
/usr/bin/sandbox-exec -f /tmp/aide-debug-rt/sandbox.sb /bin/echo "hello"

# Test claude non-interactive
/usr/bin/sandbox-exec -f /tmp/aide-debug-rt/sandbox.sb $(which claude) --version
/usr/bin/sandbox-exec -f /tmp/aide-debug-rt/sandbox.sb $(which claude) -p "say hi"
```

### Checking Denial Logs

```bash
# Run claude in background
timeout 5 /usr/bin/sandbox-exec -f profile.sb $(which claude) -p "hi" &
sleep 3

# Check kernel sandbox denials
log show --predicate "eventMessage CONTAINS 'deny'" --last 5s \
  | grep "Sandbox.*deny" \
  | grep -v "duplicate\|sentineld\|airportd"
```

### Minimal Profile That Works (non-TUI)

```scheme
(version 1)
(deny default)
(allow process-exec)
(allow process-fork)
(allow signal (target self))
(allow file-read-data (subpath "/"))
(allow file-read-metadata)
(allow file-read-xattr)
(allow file-ioctl)
(allow file-write-data
    (subpath "~/.claude")
    (subpath "~/Library/Application Support/Claude")
    (subpath "$TMPDIR")
    (subpath "$PROJECT_ROOT")
    (literal "/dev/null")
    (literal "/dev/tty")
    (regex #"^/dev/ttys[0-9]+$")
    (regex #"^/dev/pty.+$")
    (literal "/dev/dtracehelper")
)
(allow file-write-create ...)  ;; same paths as file-write-data
(allow file-write-flags ...)
(allow file-write-unlink ...)
(allow network-outbound)
(allow sysctl-read)
(allow mach-lookup)
(allow pseudo-tty)
(allow ipc-posix-shm*)
(allow ipc-posix-sem)
(allow user-preference-read)
```

This profile lets `claude -p "prompt"` work but the interactive TUI still hangs.

## Proposed Approaches Going Forward

### Option A: Invert the Model — `(allow default)` + Deny Writes

Start from `(allow default)` and only restrict writes and sensitive reads:

```scheme
(version 1)
(allow default)

;; Deny reading secrets
(deny file-read-data (literal "~/.ssh/id_*"))
(deny file-read-data (literal "~/.aws/credentials"))
;; ... other sensitive paths

;; Deny writing outside allowed paths
(deny file-write*
    (require-not
        (require-any
            (subpath "$PROJECT_ROOT")
            (subpath "$TMPDIR")
            (subpath "~/.claude")
            (subpath "~/Library/Application Support/Claude")
        )
    )
)
```

**Pros:** Won't break TUI or any OS operation. Simple. Maintainable.
**Cons:** Broader than ideal — allows operations we might want to block.
**Security:** Still prevents writing outside approved dirs and reading SSH keys/credentials.

### Option B: Skip Seatbelt on Darwin, Use Only on Linux

macOS: No OS-level sandbox (rely on agent-level permissions like `--dangerously-skip-permissions`).
Linux: Use Landlock (kernel 5.13+) or bwrap — both are modern, well-documented, and designed for application sandboxing.

**Pros:** No macOS Seatbelt fragility. Linux sandbox works well.
**Cons:** No write protection on macOS.

### Option C: Use Seatbelt Only for Network + Write Restriction

A hybrid: use `(allow default)` but deny network (for `NetworkNone` mode) and restrict writes. Don't try to restrict reads at all since the deny-list for reads works fine.

```scheme
(version 1)
(allow default)

;; Deny reading secrets
(deny file-read-data ...)

;; Deny writing outside allowed paths
(deny file-write* (require-not (require-any ...)))

;; Network restriction (only for NetworkNone mode)
(deny network*)
```

**Pros:** Write protection + secret denial. TUI works. Simple profile.
**Cons:** Requires `require-not` + `require-any` Seatbelt syntax which is less tested.

## Update (2026-03-20): Agent-Safehouse Approach Works

Further investigation found that **Option A is wrong** — `(allow default)` paradoxically hangs Claude for `-p` and TUI modes, even with zero restrictions.

The working approach is `(deny default)` with agent-safehouse's granular rules:
- Specific Mach service lookups (~15 services)
- `process-info* (target same-sandbox)` + `mach-priv-task-port (target same-sandbox)`
- `system-socket` for AF_SYSTEM sockets
- Granular `file-ioctl` restricted to device nodes
- `ipc-posix-shm` for notification center

**Verified:** `sandbox-exec` with this profile + `claude -p "say hello"` → returned response. TUI also started.

See `docs/superpowers/specs/2026-03-20-seatbelt-library-design.md` for the library design.

## Original Recommendation (superseded)

**Option A** was the original pragmatic choice. The security value of the sandbox is primarily:

1. Preventing agents from writing to arbitrary locations (malicious file creation)
2. Preventing agents from reading SSH keys and cloud credentials
3. Network restriction for offline mode

All three work with `(allow default)` + targeted denials. The deny-default model adds theoretical security (blocking unknown operations) but in practice breaks real applications and is unmaintainable against macOS updates.

## Linux Status

Linux sandboxing via bwrap works correctly. Key findings:

- bwrap mount order matters: system mounts (`--tmpfs /tmp`) must come before user binds so user paths overlay correctly
- Docker containers need `--privileged` for bwrap namespace creation
- Landlock (kernel 5.13+) is the preferred backend; bwrap is the fallback
- Integration tests pass in the devcontainer with `--privileged`

## Files Modified

| File | Changes |
|------|---------|
| `internal/sandbox/darwin.go` | Seatbelt profile generation — added OS essentials, tried deny-default then allow-default |
| `internal/sandbox/sandbox.go` | `extraWritablePaths()` for `~/.claude`, expanded `extraReadablePaths()` |
| `internal/sandbox/policy.go` | `ResolveSandboxRef()`, `ValidateSandboxRef()` for named profiles |
| `internal/config/schema.go` | `SandboxRef` type, `Sandboxes` map on Config |
| `internal/launcher/launcher.go` | Default-on sandbox, `resolveSandboxConfig()` |
| `internal/sandbox/linux.go` | Fixed bwrap mount ordering |
| `cmd/aide/commands.go` | CLI commands: `sandbox show/list/create/edit/remove` |
