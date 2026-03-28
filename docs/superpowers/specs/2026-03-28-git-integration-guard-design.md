# Git Integration Guard & Git Remote Guard

**Date:** 2026-03-28
**Status:** Approved (revised after spec review round 2)

## Problem

The filesystem guard narrowing (`022173b`) replaced a broad home dotfile regex
with explicit paths. `~/.gitignore` and `~/.gitattributes` were accidentally
dropped, breaking `git status` and other local git operations inside the
sandbox. Patching individual files is fragile — git's config system supports
includes, conditional includes, and custom paths for excludes/attributes files.
A proper solution requires understanding git's full configuration graph.

## Design Principles

1. **Self-contained** — The git guard owns all git config paths. No implicit
   dependency on the filesystem guard (per the pattern established in `c191949`).
2. **Parse, don't guess** — Use `go-git` sub-packages to parse gitconfig and
   resolve the actual file graph rather than hardcoding well-known paths.
3. **Local only** — The git-integration guard enables local git operations
   (status, log, diff, commit). Remote operations (push, fetch, pull) require
   the separate `git-remote` guard, enabled via capability.
4. **Graceful degradation** — If config parsing fails, fall back to well-known
   static paths. Never fail the sandbox build.
5. **Symlink-aware** — Resolve symlinks in discovered paths before generating
   seatbelt rules, since seatbelt `literal` does not follow symlinks.

## Component 1: `git-integration` Guard

### Type

`always` — cannot be disabled. Git config reading is foundational to every
workflow and poses zero security risk (read-only access to config files).

### File

`pkg/seatbelt/guards/guard_git_integration.go`

### Initialization Flow

1. Check for `$GIT_CONFIG_GLOBAL` and `$GIT_CONFIG_SYSTEM` env overrides via
   `ctx.EnvLookup()`. If set, use those paths instead of the defaults. Report
   as `Override` entries in `GuardResult`.
2. Use `go-git`'s `plumbing/format/config` package to parse the global
   gitconfig (and `$XDG_CONFIG_HOME/git/config`).
3. Walk `[include]` and `[includeIf]` directives with custom resolution code
   (the `plumbing/format/config` parser does not resolve includes — it only
   parses the INI structure). For `[includeIf "gitdir:..."]` conditions,
   evaluate against `ctx.ProjectRoot`. Enforce a maximum include depth of 10
   to prevent circular includes.
4. Extract `core.excludesFile` and `core.attributesFile` values.
5. Expand `~` to `ctx.HomeDir` in any parsed paths.
6. Resolve symlinks in all discovered paths before generating rules.
7. Collect all discovered file paths.
8. Emit `file-read*` allow rules for every discovered path.

### Allowed Paths (read-only)

| Source | Paths |
|--------|-------|
| Always (well-known) | `~/.gitconfig`, `~/.config/git/config`, `~/.config/git/ignore`, `~/.config/git/attributes` |
| Env override | `$GIT_CONFIG_GLOBAL` if set (replaces `~/.gitconfig`) |
| Env override | `$GIT_CONFIG_SYSTEM` if set (replaces `/etc/gitconfig`) |
| Parsed from config | `core.excludesFile` value (fallback: `~/.gitignore`) |
| Parsed from config | `core.attributesFile` value (fallback: `~/.config/git/attributes`) |
| Resolved includes | Every file referenced by `[include]` / `[includeIf]` directives |

### Fallback Behavior

If `go-git` parsing fails (corrupted config, permissions error), fall back to
the well-known static paths listed above. Log a warning. The guard must never
fail the sandbox profile build.

### Registration

Added to the "always" group in `registry.go`, placed after `FilesystemGuard()`
and before `KeychainGuard()`. Add a comment in `registry.go` explaining the
ordering rationale.

**Note on ordering:** Guard `Rules()` methods execute before the sandbox
profile is active (profile is built, then applied). File I/O during profile
building is unrestricted. The ordering only affects rule placement in the
final generated profile.

### Changes to Existing Code

- Remove the inline git config allows from `guard_filesystem.go` (lines 58-63:
  `.gitconfig` and `.config/git`). The git-integration guard owns these now.
- Update `guard_filesystem_test.go` to remove assertions for `.gitconfig` and
  `.config/git` paths (these move to git-integration guard tests).
- Add `{".git-credentials", false}` to the `credentialPaths` list in
  `guard_dev_credentials.go` for explicit denial of plaintext credential files.

## Component 2: `git-remote` Guard

### Type

`opt-in` — disabled by default. Activated when the `git-remote` capability is
enabled, which adds this guard to the active guard set.

This requires a new `EnableGuard` field on the `Capability` struct (see
Architectural Change below), since the existing `Unguard` mechanism only
removes guards — there is no current path to add opt-in guards via capability.

### Purpose

Enables git remote operations (push, fetch, pull) by allowing SSH key access,
credential helpers, and network outbound. This is a separate concern from the
git-integration guard because remote operations are an escalation — the agent
can now interact with external services.

**Security warning:** Enabling `git-remote` opens SSH and HTTPS network access
that is not scoped to specific hosts. The agent can potentially reach any
remote host on ports 22 and 443. Users should be aware of this when enabling
the capability. This is an MVP limitation — host-scoped network filtering may
be added in a future iteration if macOS Seatbelt gains hostname-based filtering
or if we implement DNS resolution at build time.

### File

`pkg/seatbelt/guards/guard_git_remote.go`

### Detection

Auto-detected in `internal/capability/detect.go`. Add a detection block in
`DetectProject()` that checks: when `.git/config` exists in the project root
and contains `[remote "..."]` sections with at least one remote URL, suggest
`git-remote` to the user. If no remotes exist, do not suggest.

### Capability Definition

Add to `builtin.go`:

```go
"git-remote": {
    Name:        "git-remote",
    Description: "Git remote operations (push, fetch, pull) via SSH and HTTPS",
    EnableGuard: []string{"git-remote"},
    EnvAllow:    []string{"SSH_AUTH_SOCK"},
},
```

### Activation Flow

1. Parse `.git/config` using `go-git` to verify remotes exist.
2. Read `SSH_AUTH_SOCK` from `ctx.EnvLookup()` to discover the agent socket
   path for the current session.
3. Generate sandbox rules for SSH file access, credential helpers, SSH agent
   socket, and network outbound.

### Generated Rules

| Concern | Rules |
|---------|-------|
| SSH keys | `file-read*` on `~/.ssh/config`, `~/.ssh/known_hosts`, `~/.ssh/id_*` |
| SSH agent | `network-outbound` on unix socket at `$SSH_AUTH_SOCK` path (read from `ctx.EnvLookup()`, skipped if not set) |
| HTTPS credentials | Keychain access (via existing keychain guard), `file-read*` on `~/.config/git-credential-manager/` if directory exists |
| Network | `network-outbound` on ports 22 (SSH) and 443 (HTTPS) — not host-scoped (see security warning) |
| Env passthrough | `SSH_AUTH_SOCK` via capability `EnvAllow` |
| Credential deny | `deny file-read-data` on `~/.git-credentials` (belt-and-suspenders with dev-credentials guard) |

### Explicit Credential Denial

The `git-remote` guard itself includes a deny rule for `~/.git-credentials`.
This provides defense-in-depth: even if `dev-credentials` guard is unguarded
by another capability, the plaintext credential file remains denied. In
seatbelt's deny-wins semantics, this deny takes precedence over any allow.

### What It Does NOT Enable

- Writing to `~/.ssh/` — read-only access
- `~/.git-credentials` — plaintext credential files explicitly denied

### Interaction with Existing `ssh` Capability

Independent. The `ssh` capability enables broad SSH access for general purposes
(interactive SSH sessions, SCP, etc.) via `Readable: []string{"~/.ssh"}`. It
does NOT include network outbound rules. `git-remote` enables both SSH file
access AND network outbound for git transport.

- `ssh` alone: can read SSH keys but no network access for git
- `git-remote` alone: can read SSH keys + network on ports 22/443
- Both: redundant file allows (ssh's broader subpath subsumes), network from
  git-remote

Users who want `git push` should enable `git-remote`. The `ssh` capability is
for non-git SSH use cases (remote shells, SCP, tunnels).

### Session Boundary

Remotes added at runtime (e.g., `git remote add`) are not picked up by the
sandbox. A new aide session is required. This is consistent with how all guards
and capabilities work — the sandbox profile is built once at session start.

## Architectural Change: `EnableGuard` on Capability

### Problem

The capability system has `Unguard` to remove guards from the active set, but
no mechanism to add opt-in guards. `git-remote` is the first guard that should
be off by default and activated by a capability.

### Data Flow

`EnableGuard` piggybacks on the existing `GuardsExtra` mechanism. The data
flows through the same pipeline as `Unguard` and `Readable`:

```
Capability.EnableGuard
  → flatten() / mergeChild() / mergeAdditive()
    → ResolvedCapability.EnableGuard
      → Set.ToSandboxOverrides()
        → SandboxOverrides.EnableGuard
          → ApplyOverrides() appends to SandboxPolicy.GuardsExtra
            → resolveGuards() picks up GuardsExtra (EXISTING logic, no changes)
```

No changes to `resolveGuards()` or `PolicyFromConfig()`. The existing
`GuardsExtra` handling already adds guards to the default set:

```go
case hasGuardsExtra:
    guardNames = append(guardNames, guards.DefaultGuardNames()...)
    for _, name := range cfg.GuardsExtra {
        expanded := guards.ExpandGuardName(name)
        guardNames = append(guardNames, expanded...)
    }
```

### File Changes

**`internal/capability/capability.go`** — Add field to structs:

```go
type Capability struct {
    // ... existing fields ...
    EnableGuard []string  // guards to activate when capability is enabled
}

type ResolvedCapability struct {
    // ... existing fields ...
    EnableGuard []string
}
```

Update `flatten()`, `mergeChild()`, `mergeAdditive()` to propagate
`EnableGuard` (same pattern as `Unguard`).

Update `ToSandboxOverrides()` to collect `EnableGuard`:

```go
func (cs *Set) ToSandboxOverrides() SandboxOverrides {
    var o SandboxOverrides
    for _, rc := range cs.Capabilities {
        // ... existing fields ...
        o.EnableGuard = append(o.EnableGuard, rc.EnableGuard...)
    }
    // ... existing dedup/filter logic ...
    o.EnableGuard = dedup(o.EnableGuard)
    return o
}
```

**`internal/config/schema.go`** — Add field to `SandboxOverrides`:

```go
type SandboxOverrides struct {
    // ... existing fields ...
    EnableGuard []string
}
```

**`internal/sandbox/capabilities.go`** — Append to `GuardsExtra` in
`ApplyOverrides()`:

```go
func ApplyOverrides(cfg **config.SandboxPolicy, overrides config.SandboxOverrides) {
    // ... existing lines ...
    (*cfg).GuardsExtra = append((*cfg).GuardsExtra, overrides.EnableGuard...)
}
```

**No changes to `policy.go`** — `resolveGuards()` already handles
`GuardsExtra`.

### Registry Change

Add "opt-in" to the recognized guard types in `registry.go`. The `typeOrder`
map already has a slot for it. Opt-in guards:
- Not included in `DefaultGuardNames()`
- Only active when explicitly added via `EnableGuard` or `guards_extra` config
- Users can also manually add opt-in guards via `guards_extra` in their
  `.aide.yaml` without using a capability

## Error Handling

| Scenario | Behavior |
|----------|----------|
| Corrupted/unreadable `~/.gitconfig` | Fall back to well-known static paths, log warning |
| Missing config fields (`core.excludesFile`) | Use defaults (`~/.gitignore`, `~/.config/git/attributes`) |
| Circular includes | Custom resolver enforces max depth of 10, logs warning |
| `$GIT_CONFIG_GLOBAL` set | Use that path instead of `~/.gitconfig`, report as Override |
| `$GIT_CONFIG_SYSTEM` set | Use that path instead of `/etc/gitconfig`, report as Override |
| Symlink in config path | Resolve to real path before generating seatbelt rule |
| No `.git/` directory | `git-remote` guard not suggested |
| Malformed remote URLs | Skip that remote, log warning, continue with others |
| No remotes configured | `git-remote` guard not suggested |
| `$SSH_AUTH_SOCK` not set | Skip SSH agent socket rule, log info |
| Tilde in paths (`~/my-ignores`) | Expand `~` to `ctx.HomeDir` before generating rules |

## New Dependency

`go-git/v5/plumbing/format/config` — Git config INI parser from go-git. Parses
the gitconfig format but does NOT resolve `[include]`/`[includeIf]` directives
— we implement custom resolution code with depth limiting.

Also used: `go-git/v5/config` for repository-level config parsing (remote URL
extraction in git-remote guard).

## Testing Strategy

### Unit Tests

**git-integration guard:**
- Config parsing: mock gitconfig with includes, conditional includes, custom
  `excludesFile`, custom `attributesFile`
- Include resolution: depth limiting, circular detection, `includeIf gitdir:`
  evaluation against project root
- Env override: `$GIT_CONFIG_GLOBAL`, `$GIT_CONFIG_SYSTEM` respected
- Symlink resolution in discovered paths
- Fallback behavior: missing config, corrupted config, missing fields
- Tilde expansion in parsed paths

**git-remote guard:**
- Remote URL parsing: SSH shorthand, HTTPS, `ssh://`, edge cases
- SSH agent socket discovery from env, graceful skip when unset
- Explicit `~/.git-credentials` deny rule present
- Detection logic: remotes present -> suggest, no remotes -> don't suggest

**Capability system:**
- `EnableGuard` field propagates through resolution pipeline
- `resolveGuards()` adds opt-in guards when capability is enabled
- Opt-in guards not included in default set without capability

### Integration Tests
- Full git-integration guard generates expected seatbelt rules given a known
  gitconfig
- git-remote guard generates SSH + network + deny rules when enabled
- Verify filesystem guard no longer emits git config paths
- Verify dev-credentials guard denies `~/.git-credentials`
- End-to-end: enable `git-remote` capability, verify guard activates

## Out of Scope

- Per-repo `.git/` directory access — covered by filesystem guard's project
  root write access
- Git hook execution policy — covered by project-secrets guard's `.git/hooks`
  write deny
- General SSH access — covered by existing `ssh` capability
- Host-scoped network filtering — MVP limitation, may be revisited
- Credential helper process execution — covered by system-runtime guard's
  existing subprocess rules
- Worktree-specific `includeIf` evaluation — `ctx.ProjectRoot` is used for
  `gitdir:` conditions; linked worktrees use the main repo's gitdir
