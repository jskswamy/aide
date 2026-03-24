# Yolo Mode Configuration Design

**Date:** 2026-03-24
**Status:** Draft
**Relates to:** Epic 6 (CLI UX), Sandbox Guards Design

## Problem

Currently `--yolo` is a CLI-only flag (default `false`) that injects agent-specific
skip-permissions flags (`--dangerously-skip-permissions` for Claude,
`--full-auto` for Codex, `--yolo` for Gemini). There is no way to:

1. Set yolo as a persistent preference in config
2. Disable yolo when it's enabled in config (no `--no-yolo` flag)
3. Control yolo per-context (e.g., trust one project but not another)

Users who have configured aide with guards and sandbox policies have already
defined their security boundary. For them, the agent's built-in permission
prompts are redundant noise. They should be able to opt into yolo persistently
without passing `--yolo` on every invocation.

## Design Decisions

### DD-25: Yolo in config with two-level override

**Decision:** Add `yolo` as a boolean field at both `preferences` (global default)
and `context` (per-context override) levels. CLI flags (`--yolo` / `--no-yolo`)
take highest precedence.

**Rationale:** Follows the existing pattern where `preferences` holds global
defaults and contexts override them (same as `show_info`, `info_style`, etc.).
Users can set a global preference and carve out exceptions per context.

**Architectural note:** `yolo` is a UX/behavior preference (like `show_info`),
not an agent-launch parameter (like `agent`, `env`, `secret`). It lives on
`Preferences` for the global default. For per-context override, it goes directly
on `Context` as a convenience — this avoids requiring a nested `preferences:`
block inside each context just for a single boolean. The resolver merges
context-level yolo with global preferences via `ResolveYolo()`.

### DD-26: Passthrough path stays opt-in

**Decision:** When no config exists (passthrough path), yolo defaults to `false`
and can only be enabled via `--yolo` CLI flag. The `--no-yolo` flag is also
respected in passthrough mode (overrides `--yolo` if both are set).

**Rationale:** Passthrough users haven't configured any security policy beyond
the `DefaultPolicy` guards. Requiring explicit `--yolo` ensures they make a
conscious choice. The same applies when using `--agent` explicitly in
passthrough mode.

### DD-27: Default remains false (yolo off)

**Decision:** The default value for `yolo` is `false` in both config and CLI.
Making yolo the default for the launch path is deferred until command-level
guards exist (see "Out of Scope" section).

**Rationale:** The current guard system operates at the filesystem/network level.
Within writable paths (project root, temp), the agent can perform destructive
operations (`rm -rf`, `git push --force`) without any prompt. Until command-level
guards fill this semantic gap, the agent's permission system remains a valuable
safety net.

### DD-28: Mutual exclusivity of `--yolo` and `--no-yolo`

**Decision:** When both `--yolo` and `--no-yolo` are provided, `--no-yolo` wins
silently (no error). This is handled by precedence order in `ResolveYolo()`,
not by cobra's `MarkFlagsMutuallyExclusive`.

**Rationale:** Shell aliases and wrapper scripts may set `--yolo` and users may
want to override with `--no-yolo` without removing the alias. Erroring on both
would break this use case. The precedence approach is simpler and more forgiving.

## Config Schema

### Preferences (global default)

```yaml
preferences:
  show_info: true
  info_style: compact
  yolo: true          # NEW: global default for yolo mode
```

### Context (per-context override)

```yaml
contexts:
  work:
    agent: claude
    match:
      - remote: "github.com/myorg/*"
    yolo: true        # NEW: override global preference for this context

  sensitive:
    agent: claude
    match:
      - path: "~/sensitive-project"
    yolo: false        # NEW: explicitly disable even if global is true
```

### Minimal (flat) format

```yaml
agent: claude
yolo: true             # NEW: promoted to synthetic default context
```

## Resolution Order

Precedence from highest to lowest:

1. If `--no-yolo` CLI flag is set → yolo = false
2. If `--yolo` CLI flag is set → yolo = true
3. If resolved context has `yolo` field set → use context value
4. If `preferences.yolo` is set → use preferences value
5. Otherwise → yolo = false

For the passthrough path (no config):

1. If `--no-yolo` CLI flag is set → yolo = false
2. If `--yolo` CLI flag is set → yolo = true
3. Otherwise → yolo = false

(No config to read, so steps 3-4 from the launch path don't apply.)

## Code Changes

### 1. Config schema (`internal/config/schema.go`)

Add `Yolo *bool` field to four structs. Use `*bool` (pointer) to distinguish
"not set" from "explicitly false", matching the existing pattern used by `ShowInfo`.

```go
// Preferences — add Yolo field
type Preferences struct {
    ShowInfo   *bool  `yaml:"show_info,omitempty"`
    InfoStyle  string `yaml:"info_style,omitempty"`
    InfoDetail string `yaml:"info_detail,omitempty"`
    Yolo       *bool  `yaml:"yolo,omitempty"`
}

// Context — add Yolo field
type Context struct {
    Match              []MatchRule          `yaml:"match,omitempty"`
    Agent              string               `yaml:"agent"`
    Secret             string               `yaml:"secret,omitempty"`
    Env                map[string]string    `yaml:"env,omitempty"`
    MCPServers         []string             `yaml:"mcp_servers,omitempty"`
    MCPServerOverrides map[string]MCPServer `yaml:"mcp_server_overrides,omitempty"`
    Sandbox            *SandboxRef          `yaml:"sandbox,omitempty"`
    Yolo               *bool                `yaml:"yolo,omitempty"`
}

// Config — add Yolo field for minimal format
type Config struct {
    // ... existing fields ...
    Yolo *bool `yaml:"yolo,omitempty"`  // minimal format, promoted to synthetic context
}

// ProjectOverride — add Yolo field
type ProjectOverride struct {
    // ... existing fields ...
    Yolo *bool `yaml:"yolo,omitempty"`
}
```

### 2. Yolo resolution (`internal/config/schema.go`)

Add `ResolveYolo` alongside the existing `ResolvePreferences` function:

```go
// ResolveYolo determines the effective yolo setting.
// cliYolo and cliNoYolo represent the --yolo and --no-yolo CLI flags.
// contextYolo is from the resolved context, globalYolo from preferences.
func ResolveYolo(cliYolo, cliNoYolo bool, contextYolo, globalYolo *bool) bool {
    if cliNoYolo {
        return false
    }
    if cliYolo {
        return true
    }
    if contextYolo != nil {
        return *contextYolo
    }
    if globalYolo != nil {
        return *globalYolo
    }
    return false
}
```

### 3. Resolver changes (`internal/context/resolver.go`)

**Minimal config promotion** — in `Resolve()`, when building the synthetic
context from minimal format, propagate `cfg.Yolo`:

```go
if cfg.IsMinimal() {
    ctx := config.Context{
        Agent:      cfg.Agent,
        Env:        cfg.Env,
        Secret:     cfg.Secret,
        MCPServers: cfg.MCPServers,
        Sandbox:    config.SandboxPolicyToRef(cfg.Sandbox),
        Yolo:       cfg.Yolo,  // NEW: promote yolo to synthetic context
    }
    // ... rest unchanged
}
```

**Project override merge** — in `applyProjectOverride()`, propagate `po.Yolo`:

```go
func applyProjectOverride(rc *ResolvedContext, po *config.ProjectOverride) {
    if po == nil {
        return
    }
    // ... existing fields ...
    if po.Yolo != nil {
        rc.Context.Yolo = po.Yolo
    }
}
```

### 4. CLI flags (`cmd/aide/main.go`)

Add `--no-yolo` flag alongside existing `--yolo`:

```go
var yolo bool
var noYolo bool

rootCmd.Flags().BoolVar(&yolo, "yolo", false,
    "Launch agent with skip-permissions (agent-specific, sandbox still applies)")
rootCmd.Flags().BoolVar(&noYolo, "no-yolo", false,
    "Disable yolo mode (overrides config and --yolo flag)")
```

Remove the yolo warning block from `main.go` (lines 34-39). The warning
moves into the launcher (see section 7).

Pass both flags to the launcher:

```go
l := &launcher.Launcher{
    Execer: &launcher.SyscallExecer{},
    Yolo:   yolo,
    NoYolo: noYolo,
}
```

### 5. Launcher struct (`internal/launcher/launcher.go`)

Add `NoYolo` field:

```go
type Launcher struct {
    Execer    Execer
    ConfigDir string
    LookPath  LookPathFunc
    Yolo      bool         // from CLI --yolo flag
    NoYolo    bool         // from CLI --no-yolo flag
    Stderr    io.Writer
}
```

### 6. Launch path (`internal/launcher/launcher.go`)

In `Launch()`, replace the current yolo injection (lines 109-116) with
config-aware resolution:

```go
// Resolve effective yolo from CLI flags + config
var globalYolo *bool
if cfg.Preferences != nil {
    globalYolo = cfg.Preferences.Yolo
}
effectiveYolo := config.ResolveYolo(l.Yolo, l.NoYolo, rc.Context.Yolo, globalYolo)

if effectiveYolo {
    yoloArgs, err := YoloArgs(agentName)
    if err != nil {
        return err
    }
    extraArgs = append(yoloArgs, extraArgs...)
}
```

### 7. Passthrough path (`internal/launcher/passthrough.go`)

Update `execAgent()` to respect `--no-yolo` in addition to `--yolo`:

```go
func (l *Launcher) execAgent(cwd, name, binary string, extraArgs []string) error {
    if l.Yolo && !l.NoYolo {
        yoloArgs, err := YoloArgs(name)
        if err != nil {
            return err
        }
        extraArgs = append(yoloArgs, extraArgs...)
    }
    // ... rest unchanged
}
```

### 8. Warning message (move from `main.go` to launcher)

Delete the yolo warning from `cmd/aide/main.go`. Add yolo source tracking
to the launcher, printed to stderr after yolo resolution:

```go
// In Launch(), after resolving effectiveYolo:
if effectiveYolo {
    source := yoloSource(l.Yolo, rc.Context.Yolo, globalYolo)
    fmt.Fprintf(l.stderr(), "\033[1;33mWARNING:\033[0m yolo mode enabled (%s)\n", source)
    fmt.Fprintln(l.stderr(), "  Agent permission checks are disabled.")
    fmt.Fprintln(l.stderr(), "  OS sandbox is active (use `aide sandbox show` to inspect).")
    fmt.Fprintln(l.stderr())
}

// In execAgent() (passthrough), when yolo is active:
if l.Yolo && !l.NoYolo {
    fmt.Fprintln(l.stderr(), "\033[1;33mWARNING:\033[0m yolo mode enabled (--yolo flag)")
    fmt.Fprintln(l.stderr(), "  Agent permission checks are disabled.")
    fmt.Fprintln(l.stderr(), "  OS sandbox is active with default policy (use `aide sandbox show` to inspect).")
    fmt.Fprintln(l.stderr())
}
```

Helper function:

```go
// yoloSource returns a human-readable description of why yolo is active.
func yoloSource(cliYolo bool, contextYolo, globalYolo *bool) string {
    if cliYolo {
        return "--yolo flag"
    }
    if contextYolo != nil && *contextYolo {
        return "context config"
    }
    if globalYolo != nil && *globalYolo {
        return "preferences config"
    }
    return "config"
}
```

### 9. Banner display (`internal/ui/banner.go`)

Add `Yolo` field to `BannerData`:

```go
type BannerData struct {
    ContextName string
    MatchReason string
    AgentName   string
    AgentPath   string
    SecretName  string
    SecretKeys  []string
    Env         map[string]string
    EnvResolved map[string]string
    Sandbox     *SandboxInfo
    Warnings    []string
    Yolo        bool              // NEW: whether yolo mode is active
}
```

In `RenderCompact()`, after the sandbox line, add yolo indicator when active:

```go
if data.Yolo {
    fmt.Fprintf(w, "   ⚡ yolo: agent permission checks disabled\n")
}
```

Similarly update `RenderBoxed()` and `RenderClean()`.

In `launcher.go`, set the field when building banner data:

```go
data.Yolo = effectiveYolo
```

## Affected Documentation

The following docs need updates after implementation:

| File | Change |
|------|--------|
| `docs/configuration.md` | Add `yolo` field to preferences and context schema docs |
| `docs/sandbox.md` | Explain relationship between yolo and sandbox guards |
| `docs/cli-reference.md` | Add `--no-yolo` flag, update `--yolo` description |
| `docs/getting-started.md` | Mention yolo as a config option in quickstart |
| `docs/contexts.md` | Show per-context yolo override examples |

## Example Configurations

### Power user: yolo globally, disable for sensitive projects

```yaml
preferences:
  yolo: true

contexts:
  default:
    agent: claude
    match:
      - path: "~/*"

  banking-app:
    agent: claude
    match:
      - remote: "github.com/myorg/banking-*"
    yolo: false
    sandbox:
      guards_extra: [cloud-aws]
```

### Conservative user: yolo only for personal projects

```yaml
contexts:
  personal:
    agent: claude
    match:
      - remote: "github.com/myuser/*"
    yolo: true

  work:
    agent: claude
    match:
      - remote: "github.com/company/*"
    # yolo not set → defaults to false
```

### Minimal config

```yaml
agent: claude
yolo: true
```

### Project override (`.aide.yaml`)

```yaml
# Override yolo for this specific project
yolo: false
```

## Out of Scope (Planned)

### Command-Level Guards

The current guard system protects at the OS level (filesystem paths, network
ports). It cannot express semantic constraints like:

- Allow `git push` but deny `git push --force`
- Allow `rm` but deny `rm -rf /`
- Allow HTTP GET but deny HTTP POST to certain endpoints
- Allow file writes but deny writes to specific filenames (e.g., `.env`)

**This gap is why the default remains `yolo: false`.** Within the sandbox's
writable paths, the agent can perform any operation without prompting. The
agent's built-in permission system is currently the only layer that can
intercept semantically destructive operations.

**Prerequisite for yolo-default:** Once command-level guards exist and cover
the critical semantic operations (force-push, recursive delete, credential
file writes), the default can safely flip to `yolo: true` for the launch path.

This work is tracked separately and includes:

1. **Command guard interface** — pattern-matching on command + args
2. **Built-in command guards** — git force-push, recursive rm, credential file writes
3. **Config schema** — `command_guards:` section with allow/deny rules
4. **Agent integration** — how command guards interact with agent permission systems

## Testing

1. **Unit tests for `ResolveYolo`** — all precedence combinations:
   - `--no-yolo` wins over `--yolo`
   - `--yolo` wins over context config
   - context config wins over preferences config
   - preferences config wins over default
   - both flags unset + both config nil → false
   - `--no-yolo` + context yolo=true → false
2. **Config parsing** — yolo in preferences, context, minimal format, project override
3. **Resolver: minimal config promotion** — `cfg.Yolo` propagated to synthetic context
4. **Resolver: project override merge** — `po.Yolo` overrides context yolo
5. **Launch path integration** — yolo resolved from config, overridden by CLI
6. **Passthrough path** — yolo only from CLI flags, `--no-yolo` respected
7. **Banner display** — yolo status shown when active, absent when inactive
8. **Warning messages** — correct source attribution (flag vs context vs preferences)
9. **Edge case: unsupported agent** — yolo enabled but agent not in `agentYoloFlags` → error
