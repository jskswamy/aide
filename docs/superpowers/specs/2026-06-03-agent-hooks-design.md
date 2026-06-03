# Agent Hooks Design

**Date:** 2026-06-03
**Status:** Draft

## Problem

Tools like `rtk` require a hook entry in each agent's config to intercept shell commands
before they reach the LLM. Installing these hooks today is manual and error-prone:

- `rtk init --auto-patch` writes only to `~/.claude/settings.json`, ignoring
  `CLAUDE_CONFIG_DIR`. Non-default profiles (`personal`, `work`) never get the hook.
- Each agent stores hooks in a different format and location (JSON, shell script,
  Python plugin). There is no cross-agent CLI surface for hook management.
- Users must repeat per-profile setup on every new machine.

Aide already owns plugin and MCP server reconciliation. Hooks follow the same pattern:
declare once in `config.yaml`, `aide sync` writes the correct config for every context.

## Goals

1. Aide manages hook injection for all supported agents via `aide sync`.
2. The config schema uses normalized, agent-agnostic event names.
3. Each agent driver translates normalized hooks to its native format.
4. Both declarative (`config.yaml`) and imperative (`aide hook add/remove/list`)
   surfaces exist.
5. Unsupported agents and unsupported events produce a warning, not an error. The
   same change applies to `SupportsPlugins` and `SupportsMCP` mismatches, which
   currently fail the sync instead of warning.

## Non-Goals

- Aide does not validate that hook commands work correctly inside the agent session.
  Whether `rtk hook claude` behaves correctly in a given profile is the command's
  responsibility.
- Per-context hook secrets or templated env vars beyond `{agent}` are out of scope
  for this iteration.

---

## Config Schema

Hooks live at the top level of `config.yaml`, at the same level as `mcp_servers`
and `plugins`.

```yaml
hooks:
  pre_tool:
    - matcher: shell
      command: rtk hook {agent}
  session_start:
    - command: bd prime
```

Aide normalizes event names and matchers. Each driver maps them to its native
vocabulary. Agents that do not support a given event warn and skip — they do not
fail the sync.

### Normalized Event Names

| Normalized | Claude | Cursor | Gemini | Copilot | Hermes |
|---|---|---|---|---|---|
| `pre_tool` | `PreToolUse` | `preToolUse` | `BeforeTool` | `PreToolUse` | `pre_tool_call` |
| `post_tool` | `PostToolUse` | `postToolUse` | — | — | — |
| `session_start` | `SessionStart` | — | — | — | — |
| `session_end` | `SessionEnd` | — | — | — | — |
| `notification` | `Notification` | — | — | — | — |
| `stop` | `Stop` | — | — | — | — |

Dashes indicate no native equivalent. Aide warns at sync time when a declared event
has no mapping for the context's agent.

### Normalized Matchers

| Normalized | Claude | Cursor | Gemini / Copilot / Hermes |
|---|---|---|---|
| `shell` | `Bash` | `Shell` | applies to all tools |
| absent | all tools | all tools | all tools |

Unknown matcher values pass through unchanged, allowing agent-specific matchers
when needed.

### Template Variables

Template variables appear in `command` strings and are substituted per context at
sync time.

| Variable | Substitution |
|---|---|
| `{agent}` | replaced with the agent name for each context |

At sync time, `rtk hook {agent}` becomes `rtk hook claude` for a Claude context,
`rtk hook gemini` for a Gemini context, and so on.

Aide validates template variable names at sync. An unknown `{variable}` produces a
warning and stops substitution for that entry.

### Per-Context Override

Contexts use the same `only` / `exclude` / `extra` delta pattern as `mcp_servers`
and `plugins`.

```yaml
contexts:
  personal:
    hooks:
      extra:
        pre_tool:
          - matcher: shell
            command: personal-specific-hook
```

The top-level `hooks` block applies to all contexts unless a context declares an
override.

---

## Data Model

### config/schema.go

```go
// HookEntry is one entry in the normalized hooks map.
type HookEntry struct {
    Matcher string `yaml:"matcher,omitempty"` // normalized matcher, e.g. "shell"
    Command string `yaml:"command"`
    Timeout int    `yaml:"timeout,omitempty"` // seconds; 0 = driver default
}

// HooksMap maps a normalized event name to its list of entries.
type HooksMap map[string][]HookEntry
```

Add to `Config`:
```go
Hooks HooksMap `yaml:"hooks,omitempty"`
```

Add to `Context` (handled in `UnmarshalYAML`, tagged `yaml:"-"`):
```go
// HooksOverride holds per-context additions and removals over the top-level
// hooks block. Extra uses HooksMap (same shape as top-level) so each extra
// entry carries a full list of HookEntry values per event, not a single entry.
// Exclude lists normalized event names to suppress from the inherited set.
type HooksOverride struct {
    Exclude []string `yaml:"exclude,omitempty"` // event names to remove
    Extra   HooksMap `yaml:"extra,omitempty"`   // events to add
}

// On Context:
Hooks *HooksOverride `yaml:"-"` // handled in UnmarshalYAML
```

### provision/provisioner.go

```go
// Hook is aide's internal, agent-neutral hook representation.
// Drivers receive a []Hook after template substitution and translate
// each entry to their native format.
type Hook struct {
    Event   string // normalized event name, e.g. "pre_tool"
    Matcher string // normalized matcher, e.g. "shell"; empty = all
    Command string // after {agent} substitution
    Timeout int    // seconds; 0 = driver default
}
```

### Template Variable Registry

```go
// TemplateVar describes one substitution variable available in hook commands.
type TemplateVar struct {
    Name        string
    Description string
}

// HookTemplateVars is the registry surfaced in CLI help and interactive prompts.
var HookTemplateVars = []TemplateVar{
    {
        Name:        "agent",
        Description: "replaced with the agent name for each context",
    },
}
```

Substitution uses `ctx.Agent`. The registry drives both `--help` output and the
interactive prompts — no separate documentation path.

---

## Capabilities

Add `SupportsHooks bool` to `provision.Capabilities` and a promoted
`SupportsHooks() bool` method to `DriverBase`. Driver constructors set it:

| Driver | SupportsHooks |
|---|---|
| claude | true |
| cursor | true |
| gemini | true |
| copilot | true |
| hermes | true |
| codex | false |
| windsurf | false |

---

## HookInstaller Interface

```go
// HookInstaller is the file-edit interface for hook management. Drivers
// implement it when no CLI surface exists for hooks — which is every agent
// today. Each driver reads its native config, translates aide's normalized
// Hook slice, merges, and writes atomically.
type HookInstaller interface {
    // ReadHooks returns the hooks currently installed by aide in this context.
    // Returns only aide-managed entries; user-added entries are left untouched.
    ReadHooks(ctx Context) ([]Hook, error)

    // WriteHooks reconciles desired into the agent's config. It adds entries
    // aide declared, removes entries aide previously declared but no longer
    // declares, and leaves all other entries untouched.
    WriteHooks(ctx Context, hooks []Hook) error
}
```

The engine checks `SupportsHooks()` first. If false, it warns and skips. If true,
it checks whether the driver implements `HookInstaller` and dispatches through it.

---

## Driver Implementations

### Claude (`internal/provision/agents/claude/hooks.go`)

- **File:** `$CLAUDE_CONFIG_DIR/settings.json` (resolved from `ctx.Env["CLAUDE_CONFIG_DIR"]`; falls back to `~/.claude`)
- **Format:** JSON — merges into `{"hooks": {"PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "..."}]}]}}`
- **Event map:** `pre_tool` → `PreToolUse`, `post_tool` → `PostToolUse`, `session_start` → `SessionStart`, `session_end` → `SessionEnd`, `notification` → `Notification`, `stop` → `Stop`
- **Matcher map:** `shell` → `Bash`; absent → omit matcher field

### Cursor (`internal/provision/agents/cursor/`) — new driver

Cursor has no existing aide driver. This spec introduces the package. The initial
driver implements only `HookInstaller`; plugin and MCP support are out of scope.

#### `hooks.go`

- **File:** `~/.cursor/hooks.json`
- **Format:** `{"version": 1, "hooks": {"preToolUse": [{"command": "...", "matcher": "Shell"}]}}`
- **Event map:** `pre_tool` → `preToolUse`
- **Matcher map:** `shell` → `Shell`

### Gemini (`internal/provision/agents/gemini/hooks.go`)

- **Files:** shell script at `~/.gemini/hooks/<name>.sh` plus a `BeforeTool` entry in `~/.gemini/settings.json`
- **Format:** shell script `#!/bin/bash\nexec <command>`; JSON settings entry references the script path
- **Event map:** `pre_tool` → `BeforeTool` via shell script
- **Matcher:** none (Gemini applies the hook to all tools)

### Copilot (`internal/provision/agents/copilot/hooks.go`)

- **File:** `.github/hooks/<name>.json` (project-level) or global equivalent
- **Format:** `{"hooks": {"PreToolUse": [{"type": "command", "command": "...", "timeout": 5}]}}`
- **Event map:** `pre_tool` → `PreToolUse`

### Hermes (`internal/provision/agents/hermes/`) — new driver

Hermes has no existing aide driver. Same scope constraint as Cursor: hooks only
for this iteration.

#### `hooks.go`

- **Files:** `~/.hermes/plugins/<name>/__init__.py` and `plugin.yaml`
- **Format:** Python plugin; `plugin.yaml` declares `hooks: [pre_tool_call]`
- **Event map:** `pre_tool` → `pre_tool_call`

---

## Ownership Tracking

Aide tracks managed hooks in `managed.json` per context, alongside plugins and MCP
servers.

```json
{
  "contexts": {
    "personal": {
      "hooks": [
        {"event": "pre_tool", "matcher": "shell", "command": "rtk hook claude"}
      ]
    }
  }
}
```

On sync, aide adds declared hooks absent from the agent's config, removes
aide-managed hooks no longer declared, and leaves user-added hooks untouched.
Identity for deduplication is `(event, matcher, command)`.

---

## Engine Changes

### Warn-and-Skip Policy

Replace `fmt.Errorf` with a warning + skip for all capability mismatches in
`cmd/aide/sync.go`:

```
warning: agent "gemini" does not support hooks (2 declared) — skipping
warning: agent "codex" does not support plugins (3 declared) — skipping
warning: agent "copilot" does not support MCP servers (1 declared) — skipping
```

The same change applies to the existing `SupportsPlugins` and `SupportsMCP` checks,
which currently fail the sync. Sync continues for all capabilities the agent does
support.

### Plan and Apply

1. Add `KindHook` to the `Kind` enum.
2. `ResolveDesired` resolves hooks from config, applies per-context overrides,
   substitutes `{agent}` using `ctx.Agent`, and warns on unknown template variables.
3. `ComputePlan` diffs desired hooks against the managed set (from `managed.json`).
4. `Apply` dispatches `KindHook` ops through `HookInstaller`. On failure it rolls
   back via the journal, consistent with plugin and MCP rollback behavior.

---

## Imperative CLI Surface

### `aide hook add`

Flags:
```
--event    normalized event name (pre_tool, post_tool, session_start, …)
--matcher  normalized matcher (shell, or omit for all tools)
--command  command string; may contain template variables
--context  target context (default: all contexts)
```

Interactive mode (flags omitted):

```
Event? [pre_tool, post_tool, session_start, session_end, notification, stop]
> pre_tool

Matcher? [shell, all tools]  (enter for all tools)
> shell

Command?
  Template variables (substituted automatically at sync time):
    {agent}  replaced with the agent name for each context
> rtk hook {agent}

Apply to? [all contexts]
> all contexts

Added hook. Run `aide sync` to apply.
```

The interactive prompt lists template variables with descriptions drawn from
`HookTemplateVars`. It never shows example values alongside variable names —
examples read as a choice list, which misleads users into writing a literal agent
name instead of the template.

The command writes the entry to `config.yaml` and prints the sync reminder. It does
not auto-sync, consistent with how `aide adopt` works today.

### `aide hook remove`

```
aide hook remove --event pre_tool --command "rtk hook {agent}"
```

Removes the matching entry from `config.yaml`.

### `aide hook list`

Prints declared hooks from `config.yaml` alongside their installed state per
context, in the same style as `aide plugin list` and `aide mcp list`.

---

## Testing

- Unit tests for each driver's event/matcher translation and `settings.json` merge
  logic, with round-trip read → write → read assertions.
- Unit tests for `{agent}` substitution and unknown-variable warning.
- Integration test: `aide sync` on a three-context config writes the correct hook
  entry to each profile's config file.
- Warn-and-skip tests: verify sync continues and prints the correct warning when
  the agent does not support hooks, plugins, or MCP.
