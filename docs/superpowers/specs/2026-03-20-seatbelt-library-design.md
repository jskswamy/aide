# pkg/seatbelt — Composable macOS Seatbelt Profile Library

**Date:** 2026-03-20
**Status:** Draft
**Scope:** New `pkg/seatbelt/` package in aide repo

## Problem

macOS Seatbelt (`sandbox-exec`) is the only OS-level sandbox mechanism on macOS. Building a working Seatbelt profile for AI coding agents (Claude Code, Codex, Cursor) requires discovering hundreds of specific macOS operations, Mach services, and file paths — a process that took multiple sessions and extensive debugging to get right.

This work should be reusable as a library so other Go projects can sandbox macOS processes without repeating the same trial-and-error.

### Key Findings

**Investigation timeline (`docs/sandbox-findings.md` + this session):**

1. `(deny default)` with basic allows → SIGABRT and TUI hang (missing operations)
2. `(allow default)` with targeted denies → `--version` works but `-p` and TUI hang
3. `(deny default)` with agent-safehouse-style **granular** Mach services/IPC/PTY → **both `-p` and TUI work**

The earlier conclusion that `(deny default)` was "unmaintainable" was wrong — the issue was missing specific Mach service lookups (`com.apple.SecurityServer`, `com.apple.trustd.agent`, etc.), `process-info*`, `mach-priv-task-port`, and `system-socket`. Agent-safehouse (github.com/eugene1g/agent-safehouse) has discovered and validated these rules for 14 agents.

**Verified on this machine (2026-03-20):**
- `sandbox-exec` with agent-safehouse-style `(deny default)` profile + `claude -p "say hello"` → returned "Hello!"
- TUI mode started and processed `/exit` command
- `(allow default)` with zero restrictions → `-p` still hung (sandbox-exec infrastructure issue)

## Solution

A composable Go library at `pkg/seatbelt/` that lets consumers build Seatbelt profiles from modular components:

```go
profile := seatbelt.New(homeDir).Use(
    modules.Base(),
    modules.SystemRuntime(),
    modules.Network(modules.NetworkOpen),
    modules.Filesystem(modules.FilesystemConfig{
        Writable: []string{projectRoot, tmpDir},
        Denied:   []string{"~/.ssh/id_*"},
    }),
    modules.NodeToolchain(),
    modules.GitIntegration(),
    modules.ClaudeAgent(),
)
sbText, _ := profile.Render()
```

### Design Principles

1. **Composable** — Pick the modules you need. Don't pay for what you don't use.
2. **Deny-default** — All profiles start with `(deny default)`. Modules add specific allows.
3. **Opinionated defaults** — `SystemRuntime()` includes the ~120 rules needed for any CLI process on macOS. This is the hard-won knowledge.
4. **Portable** — `HOME_DIR` resolved at render time. Profiles work for any user.
5. **Testable** — Each module can render independently for unit testing.

## Package Structure

```
pkg/seatbelt/
├── profile.go           # Profile builder: New(), Use(), Render()
├── module.go            # Module interface, Rule type, Context
├── render.go            # .sb format rendering (version header, comments, rules)
├── profile_test.go      # Integration tests for composed profiles
│
├── modules/
│   ├── base.go          # (version 1)(deny default) + HOME_DIR macro
│   ├── system.go        # Process, IPC, Mach services, devices, temp dirs
│   ├── network.go       # NetworkOpen, NetworkOutbound, NetworkNone, port filtering
│   ├── filesystem.go    # Writable paths, denied paths (read + write deny)
│   │
│   ├── toolchains/
│   │   ├── node.go      # npm, yarn, pnpm, corepack cache dirs
│   │   ├── nix.go       # /nix/store, ~/.nix-profile, symlink chain
│   │   ├── go.go        # GOPATH, GOMODCACHE, ~/go
│   │   ├── python.go    # pyenv, pip, uv, venv
│   │   └── rust.go      # ~/.cargo
│   │
│   ├── integrations/
│   │   ├── git.go       # .gitconfig, .ssh/config, known_hosts
│   │   ├── keychain.go  # macOS Keychain, SecurityServer Mach services
│   │   └── shell.go     # Shell init files (.bashrc, .zshrc, etc.)
│   │
│   └── agents/
│       ├── claude.go    # Claude Code: ~/.claude, ~/.local/share/claude, etc.
│       ├── codex.go     # OpenAI Codex paths
│       └── cursor.go    # Cursor agent paths
│
└── testdata/            # Golden file tests for rendered profiles
```

## Core Types

### `profile.go`

```go
package seatbelt

// Profile composes Seatbelt modules into a complete .sb profile.
type Profile struct {
    modules []Module
    ctx     Context
}

// New creates a profile builder.
func New(homeDir string) *Profile {
    return &Profile{
        ctx: Context{HomeDir: homeDir},
    }
}

// Use adds modules to the profile.
func (p *Profile) Use(modules ...Module) *Profile {
    p.modules = append(p.modules, modules...)
    return p
}

// WithContext sets additional context fields.
func (p *Profile) WithContext(fn func(*Context)) *Profile {
    fn(&p.ctx)
    return p
}

// Render generates the Seatbelt .sb profile string.
func (p *Profile) Render() (string, error) {
    // Renders all modules in order, with section comments
}
```

### `module.go`

```go
package seatbelt

// Module contributes Seatbelt rules to a profile.
type Module interface {
    // Name returns a human-readable name for section comments.
    Name() string
    // Rules returns the Seatbelt rules this module contributes.
    Rules(ctx *Context) []Rule
}

// Context provides runtime information to modules.
type Context struct {
    HomeDir     string
    ProjectRoot string
    TempDir     string
    RuntimeDir  string
}

// HomePath returns homeDir + relative path.
func (c *Context) HomePath(rel string) string {
    return filepath.Join(c.HomeDir, rel)
}

// Rule represents one or more Seatbelt rule lines.
// Most real rules are multi-line (nested filters), so Line is a
// complete text block, not a single line.
type Rule struct {
    Comment string // optional section comment (;; ...)
    Lines   string // Seatbelt rule text (may be multi-line)
}

// Helpers for common patterns. For complex nested rules, use Raw().
func Allow(operation string) Rule
func Deny(operation string) Rule
func Comment(text string) Rule
func Raw(text string) Rule     // multi-line Seatbelt blocks
func Section(name string) Rule // ;; --- section header ---
```

### `render.go`

```go
package seatbelt

// render.go handles the .sb format output.
// - Groups rules by module with section headers
// - Resolves HOME_DIR in paths
// - Wraps long lines appropriately
```

## Module Specifications

### `modules.Base()`

Emits the version header and deny-default:

```scheme
(version 1)
(deny default)
```

### `modules.SystemRuntime()`

The core module — emits ~120 rules covering everything a CLI process needs on macOS. Ported from agent-safehouse's `10-system-runtime.sb`:

- System binary paths (`/usr`, `/bin`, `/sbin`, `/opt`, `/System/Library`, etc.)
- Private paths (`/private/var/db/timezone`, `/private/etc/ssl`, etc.)
- Process rules (`process-exec`, `process-fork`, `signal`, `process-info*`)
- Mach service lookups (from agent-safehouse `10-system-runtime.sb`):
  - `com.apple.system.notification_center`
  - `com.apple.system.opendirectoryd.libinfo`
  - `com.apple.logd`
  - `com.apple.FSEvents`
  - `com.apple.SystemConfiguration.configd`
  - `com.apple.SystemConfiguration.DNSConfiguration`
  - `com.apple.trustd.agent`
  - `com.apple.diagnosticd`
  - `com.apple.analyticsd`
  - `com.apple.dnssd.service`
  - `com.apple.CoreServices.coreservicesd`
  - `com.apple.DiskArbitration.diskarbitrationd`
  - `com.apple.analyticsd.messagetracer`
  - `com.apple.system.logger`
  - `com.apple.coreservices.launchservicesd`
- Temp dirs (`/tmp`, `/private/tmp`, `/var/folders`)
- Device nodes (`/dev/null`, `/dev/tty`, `/dev/ptmx`, PTY regex patterns)
- File-ioctl (restricted to device nodes)
- IPC (`ipc-posix-shm-read-data` for notification center)
- PTY (`pseudo-tty`)
- System socket (`system-socket`)
- User preferences (`user-preference-read`)

### `modules.Network(mode)`

```go
func Network(mode NetworkMode) Module
```

Modes:
- `NetworkOpen` — `(allow network*)` (default for agents)
- `NetworkOutbound` — `(allow network-outbound)` only
- `NetworkNone` — no network rules (deny default covers it)

Optional port filtering:
```go
func NetworkWithPorts(mode NetworkMode, opts PortOpts) Module
```

### `modules.Filesystem(cfg)`

```go
type FilesystemConfig struct {
    Writable []string  // paths with read+write access
    Readable []string  // paths with read-only access
    Denied   []string  // paths denied for both read and write (secrets)
}
```

- Writable paths: `(allow file-read* file-write* (subpath "..."))`
- Readable paths: `(allow file-read* (subpath "..."))`
- Denied paths: `(deny file-read-data ...)(deny file-write* ...)` — emitted last for precedence
- Globs expanded via `filepath.Glob()`

### Toolchain modules

Each returns a `Module` that adds read+write rules for the toolchain's cache/config dirs.

Example `modules.NodeToolchain()`:
- `~/.npm`, `~/.config/npm`, `~/.cache/npm`
- `~/.yarn`, `~/.config/yarn`, `~/.cache/yarn`
- `~/.pnpm-store`, `~/.local/share/pnpm`
- Node version managers (`~/.nvm`, `~/.fnm`)
- All from agent-safehouse's `30-toolchains/node.sb`

Example `modules.NixToolchain()`:
- `/nix/store` (read)
- `/nix/var` (read)
- `/run/current-system` (read)
- `~/.nix-profile` (read)
- `~/.local/state/nix` (read — for symlink chain resolution)

### Integration modules

`modules.GitIntegration()`:
- `~/.gitconfig`, `~/.gitignore`, `~/.config/git` (read)
- `~/.ssh/config`, `~/.ssh/known_hosts` (read)

`modules.KeychainIntegration()`:
- `~/Library/Keychains` (read+write)
- System keychain paths
- SecurityServer Mach services (5 services)
- AppleDatabaseChanged shared memory

`modules.ShellInit()`:
- Shell RC files: `.bashrc`, `.zshrc`, `.profile`, `.bash_profile`, etc.
- `.zsh_history`, `.bash_history` (read-only)

### Agent modules

`modules.ClaudeAgent()`:
- `~/.claude`, `~/.config/claude`, `~/.local/share/claude`, `~/.local/state/claude` (read+write)
- `~/.claude.json`, `~/.mcp.json` (read+write)
- `~/Library/Application Support/Claude` (read)
- `/Library/Application Support/ClaudeCode` (read)

## How aide Consumes It

`internal/sandbox/darwin.go` becomes a thin adapter:

```go
func generateSeatbeltProfile(policy Policy) (string, error) {
    homeDir, _ := os.UserHomeDir()

    p := seatbelt.New(homeDir).
        WithContext(func(ctx *seatbelt.Context) {
            // Set from Policy fields
        }).
        Use(
            modules.Base(),
            modules.SystemRuntime(),
            networkModule(policy),
            modules.Filesystem(modules.FilesystemConfig{
                Writable: policy.Writable,
                Denied:   policy.Denied,
            }),
            // Auto-detect installed toolchains
            modules.NodeToolchain(),
            modules.NixToolchain(),
            modules.GitIntegration(),
            modules.KeychainIntegration(),
            modules.ClaudeAgent(),
        )

    return p.Render()
}
```

## Testing Strategy

1. **Unit tests per module** — each module renders independently, assert specific rules present
2. **Golden file tests** — full composed profiles compared against expected `.sb` files in `testdata/`
3. **Negative tests** — verify denied paths produce `(deny file-read-data ...)` rules; verify module ordering puts denies after allows
4. **Integration test** (`//go:build integration && darwin`) — generate profile, run `sandbox-exec -f profile.sb claude --version`
5. **Seatbelt syntax validation** — `sandbox-exec -f profile.sb /usr/bin/true` verifies the profile is syntactically valid
6. **Conflict detection test** — verify that `Filesystem.Denied` for `~/.ssh/id_*` coexists correctly with `GitIntegration` allowing `~/.ssh/config` (they target different files, no conflict)

## Module Ordering and Precedence

Seatbelt evaluates rules in order. The library renders modules in the order they are added via `Use()`. The convention:

1. `Base()` first — `(deny default)`
2. `SystemRuntime()` — broad process/IPC/device allows
3. `Network()` — network rules
4. `Filesystem()` writable paths — `(allow file-read* file-write* ...)`
5. Toolchain/integration modules — additional file allows
6. Agent modules — agent-specific allows
7. `Filesystem()` denied paths — `(deny file-read-data ...)` emitted **last** so they override earlier allows

The `Filesystem` module handles this internally — writable/readable rules are emitted when the module renders, but denied rules are deferred to the end of the profile. This ensures deny-list semantics work correctly under Seatbelt's last-match-wins behavior.

## AllowSubprocess and CleanEnv

These `Policy` fields are handled at the **consumer level** (aide's `darwin.go`), not in the library:

- `AllowSubprocess`: `SystemRuntime()` always includes `(allow process-fork)`. If aide needs to deny subprocesses, the adapter adds `(deny process-fork)` after all modules.
- `CleanEnv`: This is an env-var filter applied to `cmd.Env` before exec. It has nothing to do with Seatbelt rules — it stays in aide's `darwin.go` adapter.

## Migration from Current darwin.go

The current `darwin.go` uses `(allow default)` with a `require-not` write restriction. The migration:

1. Add `pkg/seatbelt/` with all modules
2. Rewrite `generateSeatbeltProfile()` in `darwin.go` to compose seatbelt modules
3. Remove the `(allow default)` profile entirely
4. Keep `Apply()` and `GenerateProfile()` methods unchanged — only the profile content changes
5. Keep `CleanEnv` handling in `Apply()`

## What This Does NOT Cover

- Linux Landlock/bwrap — separate concern, stays in `internal/sandbox/linux.go`
- Dynamic worktree detection — future enhancement
- Per-agent profile selection via config — aide currently runs one agent
- Profile caching — profiles are cheap to generate
- `agents/codex.go` and `agents/cursor.go` — listed in structure but implement only when there's a consumer. Start with `claude.go` only.
- Homebrew toolchain module — `/opt/homebrew` is covered by `SystemRuntime()` read rules. A dedicated module can be added if Homebrew-specific write paths are needed.
- Docker socket access — future optional integration module

## Attribution

The Seatbelt rules in this library — particularly the system runtime operations, Mach service lookups, toolchain paths, and integration profiles — are ported from [agent-safehouse](https://github.com/eugene1g/agent-safehouse) by Eugene Goldin. Agent-safehouse provides composable Seatbelt policy profiles for AI coding agents and has validated profiles for 14 agents including Claude Code, Codex, Cursor, and others. The original profiles are in the `profiles/` directory of that repository.

This attribution should appear in:
- `pkg/seatbelt/doc.go` — package-level documentation
- `pkg/seatbelt/modules/system.go` — file header comment (this is where the bulk of the ported rules live)
- `README.md` or equivalent library documentation if created
