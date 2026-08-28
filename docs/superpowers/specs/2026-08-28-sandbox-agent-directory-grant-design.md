# Sandbox Allow/Deny: Agent Directory Grant

## Problem

Today, giving an agent access to a new directory takes two manual steps:

1. `aide sandbox allow <dir>` — grants the path at the OS sandbox level (Landlock/Seatbelt), via `readable_extra`/`writable_extra` in aide's own config.
2. Manually editing the agent's own settings file (for Claude: `permissions.additionalDirectories` in `.claude/settings.local.json` or `~/.claude/settings.json`) so the agent's *own* internal permission layer also allows tool access to that path.

The OS sandbox and the agent's internal permission model are two independent gates; aide only closes the first one. Step 2 is easy to forget, and there's no reciprocal cleanup on `aide sandbox deny`.

## Goals

- `aide sandbox allow <dir>` also grants the directory in the underlying agent's own permission store, when the agent supports it.
- `aide sandbox deny <dir>` reciprocally revokes it.
- Implemented through each agent's own CLI where one exists; falls back to direct config-file editing (matching the existing pattern already used for hooks/statusline) where it doesn't.
- Each agent's behavior is its own, isolated implementation — agents that don't support this are unaffected and don't need code changes.
- One-time action at `sandbox allow`/`sandbox deny` time — not injected on every agent launch (avoids adding launch-time latency for something that only needs to happen once).
- Escape hatch: a flag to skip the agent-side grant/revoke entirely.

## Non-goals

- No launch-time flag injection (e.g. `--add-dir` at every `claude` invocation). Considered and rejected: it would re-run on every launch for no benefit once the grant is persisted, and adds agent-specific argument-building to the launcher's hot path.
- No change to `sandbox create`/`edit`/`remove`/named-profile commands — scoped to the two path-mutating commands, `allow` and `deny`.
- No support for agents other than Claude in this pass. The extension point is generic, but only the `claude` driver implements it — YAGNI for agents nobody has asked for yet.

## Background: why file-editing, not CLI, for Claude

Claude Code's CLI has no persistent "add directory" command. `--add-dir <path>` exists but is session-scoped: it only affects the invocation it's passed to, and is not written anywhere. `claude project`, `claude config`, etc. don't expose a way to add to `permissions.additionalDirectories` from outside a running session.

This isn't a new problem for aide: `internal/provision/agents/claude/hooks.go` already manages settings Claude has no CLI for (hooks, statusline) by reading `settings.json`, mutating the in-memory map, and writing it back atomically (`readSettings`, `WriteHooks`, `WriteStatusLine`). The directory-grant feature follows that exact precedent rather than introducing a new mechanism.

Where an agent *does* expose a persistent CLI surface for this in the future, its driver should use that instead — the interface doesn't care how a driver satisfies it.

## Design

### Extension point

`internal/provision`'s `Provisioner` interface already uses a capability-flag + interface-method pattern for optional per-agent behavior (`SupportsHooks` / `WriteHooks`, `SupportsMCP` / `MCPHandler`, `SupportsPlugins` / `InstallPlugin`), with `DriverBase` supplying no-op stubs so agents that don't implement a capability keep compiling unchanged. This feature follows the same shape:

```go
// Capabilities gains:
SupportsDirectoryGrant bool

// Provisioner gains:
GrantDirectory(ctx provision.Context, path string, write bool) error
RevokeDirectory(ctx provision.Context, path string) error
```

`DriverBase` implements both as no-ops (or a clear "unsupported" sentinel error that callers treat as "skip silently"). Only the `claude` driver sets `SupportsDirectoryGrant: true` and implements both methods for real, in a new `internal/provision/agents/claude/directories.go` alongside `hooks.go`.

`write` is passed through for parity with `sandbox allow --write` but Claude's `additionalDirectories` doesn't distinguish read/write access, so the claude driver ignores it. It's there so a future agent whose permission model *does* distinguish read/write extra paths can use it.

### `claude` driver implementation

**`GrantDirectory`**: read `settings.json` (global scope) or `settings.local.json` (project scope — same file `hooks.go`/`ReadHooks` already targets for that scope) via the existing `readSettings` helper. Append `path` to `permissions.additionalDirectories` if not already present (exact string match). Write back only if the list changed. Same atomic-write helper already used by `WriteHooks`/`WriteStatusLine`.

**`RevokeDirectory`**: read the same file. Remove any entry in `additionalDirectories` that is either an exact match for `path` or nested under it. Containment is checked with `filepath.Rel(path, entry)` and rejecting results that are `".."` or start with `"../"` — the same technique already used in `pkg/seatbelt/guards/guard_git_integration.go:183` for subpath checks, rather than a naive string-prefix comparison (which breaks on sibling paths that share a prefix, e.g. `/foo/bar` vs `/foo/barbaz`). A denied path does **not** remove any entry that is merely a *parent* of it — only the denied path itself and anything nested under it. Write back only if the list changed.

### Wiring into `cmd/aide/sandbox.go`

Both `sandboxAllowCmd` (line 664) and `sandboxDenyCmd` (line 633) gain a `--no-agent-grant` bool flag (default `false`), matching the existing `--no-yolo` negation-flag naming convention.

After each command performs its existing aide-side config mutation (`ReadableExtra`/`WritableExtra` for allow, `DeniedExtra` for deny), and if `--no-agent-grant` was not passed:

1. Resolve the target context's `Agent` (already available — this is the same context object the command already resolved for the aide-side mutation).
2. Look up the registered `Provisioner` for that agent name (same lookup the launcher already performs to get a driver).
3. If no driver is registered (agent not installed/known) or `SupportsDirectoryGrant` is false, skip silently — no error, no output.
4. Otherwise call `GrantDirectory`/`RevokeDirectory` with the same path and the same global/project scope already selected by `--global`/`--context`.

The agent-side call is best-effort on top of the aide-side write, which is the source of truth: if `GrantDirectory`/`RevokeDirectory` returns an error, print a warning to stderr but do not fail the command — the OS-level sandbox grant already succeeded and that's the security-relevant part.

### Output

On success, an extra line after the existing success message, e.g.:

```
Added /path/to/dir to readable_extra for context "work" (global)
Added /path/to/dir to Claude's additionalDirectories
```

Nothing extra is printed when skipped (unsupported agent, driver not found, or `--no-agent-grant`).

### Testing

- `internal/provision/agents/claude/directories_test.go`: table tests for `GrantDirectory` (fresh settings file, existing file with unrelated keys, path already present → no duplicate, project vs global path resolution) and `RevokeDirectory` (exact match removed, nested path removed, sibling path with shared prefix untouched, parent-of-denied-path untouched, no-op when nothing matches).
- `cmd/aide/sandbox_test.go`: `sandboxAllowCmd`/`sandboxDenyCmd` tests using a fake/mock `Provisioner` (existing `internal/provision` test doubles, or a small local fake) asserting `GrantDirectory`/`RevokeDirectory` is called with the expected path and scope, and asserting it is *not* called when `--no-agent-grant` is passed.
