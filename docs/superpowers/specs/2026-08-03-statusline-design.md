# Aide Statusline Design

**Status:** Approved
**Date:** 2026-08-03

---

## Problem

When `aide` launches a coding agent the terminal switches to the agent's
full-screen TUI. Any warnings or config information printed by aide before
the handoff get cleared and are no longer visible. There is no persistent
surface showing the active aide configuration during a session.

Claude Code's statusline feature (`statusLine` in `settings.json`) provides
a persistent bar at the bottom of the UI that runs any shell command and
renders its stdout. This is the right surface for aide to emit its session
state.

---

## Design Overview

`aide statusline <agent>` is a dual-mode subcommand:

- **Render mode** (stdin is a pipe): reads JSON from Claude Code + `AIDE_*`
  env vars, renders the configured modules to stdout. This is the command
  Claude Code invokes on every update.
- **Install mode** (`--install` flag): patches `~/.claude/settings.json` to
  set `statusLine.command` to `"aide statusline claude"`.

Aide injects `AIDE_*` env vars before exec'ing the agent. Because env vars
are inherited through the exec chain, the statusline process (a child of
claude) receives them without any file-based IPC.

```
aide launch
  → set AIDE_SANDBOX=on, AIDE_NETWORK_MODE=outbound, …
  → exec claude (env inherited)
    → claude runs "aide statusline claude" on each update
      → reads JSON stdin + AIDE_* env → renders modules → stdout
```

---

## Config Schema

The `statusline:` block lives at the top level of both the global aide
config and `.aide.yaml`. Merge is field-by-field per module; a project
config that sets a module key wins over the global value for that key only.

### Global config (`~/.config/aide/config.yaml`)

```yaml
statusline:
  order: [sandbox, network, caps, trust, context]
  sandbox:
    on: "🔒"
    off: "🔓"
    disabled: false
  network:
    outbound: "🌐"
    unrestricted: "🌍"
    disabled: false
  caps:
    icon: "⚡"       # prefix; value is comma-joined capability list
    disabled: false
  trust:
    untrusted: "⚠️"  # only rendered when .aide.yaml is untrusted
    disabled: false
  context:
    icon: "📁"
    disabled: false  # hidden automatically when no context matched
  agent:
    icon: "🤖"
    disabled: true   # opt-in; off by default
  auto_approve:
    value: "🚨"      # always rendered when active; disabled: true has no effect
```

### Project override (`.aide.yaml`)

Project configs set only what they want to change:

```yaml
statusline:
  trust:
    disabled: true   # known-trusted repo; suppress the indicator
  caps:
    icon: "🎯"       # project-specific caps icon
  order: [sandbox, caps, network]   # reorder for this project
```

### Merge rules

| Key | Merge behaviour |
|---|---|
| `order` | Project replaces global wholesale when set; global order used otherwise |
| Per-module keys | Field-by-field: project value wins over global, global wins over built-in default |
| `auto_approve.disabled` | Ignored; `auto_approve` is always rendered when active — it is a security signal |

---

## Built-in Modules

| Module | States / format | Default value | On by default |
|---|---|---|---|
| `sandbox` | `on`, `off` | `🔒` / `🔓` | yes |
| `network` | `outbound`, `unrestricted` | `🌐` / `🌍` | yes |
| `caps` | `{icon} {comma-list}` — hidden when list is empty | `⚡` | yes |
| `trust` | `untrusted` only — hidden when trusted | `⚠️` | yes |
| `context` | `{icon} {name}` — hidden when no context matched | `📁` | yes |
| `auto_approve` | single value — hidden when not active | `🚨` | forced when active |

`agent` module is out of scope for v1 — see Out of Scope.

**Icon-only defaults.** State modules (`sandbox`, `network`, `trust`,
`auto_approve`) default to icon-only. Users who want text add it directly
to the configured string:

```yaml
statusline:
  sandbox:
    on: "🔒 on"
    off: "🔓 off"
```

**Empty string hides a state.** Setting any state value to `""` suppresses
that state silently. For example, `sandbox.off: ""` makes the module
disappear when sandbox is disabled rather than showing the off icon.

---

## Sample Output

Default (sandbox on, network outbound, caps active, config trusted):
```
🔒 | 🌐 | ⚡ k8s,docker
```

Untrusted `.aide.yaml`:
```
🔒 | 🌐 | ⚡ none | ⚠️
```

Sandbox off, unrestricted network, auto-approve active:
```
🚨 | 🔓 | 🌍 | ⚡ none
```

User-added text + custom icons:
```yaml
statusline:
  sandbox:
    on: "🔒 on"
    off: "🔓 sandbox off"
  network:
    unrestricted: "🌍 open network"
```
```
🔒 on | 🌐 | ⚡ k8s,docker
```

Modules are joined by ` | `. Modules that are disabled, empty, or whose
current state maps to `""` are omitted entirely from the output.

---

## Env Var Injection

Aide sets these vars before exec'ing the agent. They are inherited through
the exec chain and available to the statusline process.

| Variable | Values |
|---|---|
| `AIDE_SANDBOX` | `on` / `off` |
| `AIDE_NETWORK_MODE` | `outbound` / `unrestricted` |
| `AIDE_CAPS` | comma-joined list; empty string when no caps active |
| `AIDE_TRUST` | `trusted` / `untrusted` |
| `AIDE_AUTO_APPROVE` | `1` when active; absent otherwise |
| `AIDE_CONTEXT` | resolved context name; absent when no context matched |
| `AIDE_AGENT` | agent binary name |

These vars are injected in `internal/launcher/launcher.go` just before
`l.Execer.Exec(binary, args, env)`.

---

## Rendering

`aide statusline claude` in render mode (stdin is a pipe):

1. Drain stdin to discard (avoids broken pipe; JSON available for future modules).
2. Read `AIDE_*` env vars.
3. Load merged config: global aide config → project `.aide.yaml` override.
4. Walk `order`, skip disabled and empty modules, render each to a string.
5. Join with ` | `, write to stdout.

---

## Install and Remove

```bash
aide statusline claude --install   # patches ~/.claude/settings.json
aide statusline claude --remove    # removes the statusLine entry (or aide's line from wrapper)
```

### Install behaviour

`--install` reads `~/.claude/settings.json` via the existing `readSettings`
infrastructure in `internal/provision/agents/claude/hooks.go`, patches it,
and writes back atomically via `fsutil.AtomicWrite`.

| Existing `statusLine.command` | Action |
|---|---|
| Not set | Set to `"aide statusline claude"` |
| `"aide statusline claude"` | No-op; print confirmation |
| Anything else | Generate wrapper script; update command to wrapper |

### Wrapper script

When a statusline command is already configured, `--install` generates
`~/.config/aide/statusline-wrapper.sh`:

```bash
#!/bin/bash
# Managed by aide statusline --install. Do not edit manually.
input=$(cat)
echo "$input" | <existing-command>
echo "$input" | aide statusline claude
```

The wrapper is made executable (`0700`) and `settings.json` is updated to
point to it. The existing command is preserved and continues to run; aide's
output appears as an additional line below it.

### Remove behaviour

Delete the `statusLine` key from `settings.json`. If the command pointed to
the aide-generated wrapper, print the wrapper path so the user can clean it
up manually (`~/.config/aide/statusline-wrapper.sh`).

---

## Implementation Locations

| Component | Location |
|---|---|
| `statusline` subcommand | `cmd/aide/statusline.go` (new) |
| Config schema (`StatuslineConfig`) | `internal/config/schema.go` |
| Config merge | `internal/config/schema.go` (`ResolveStatusline`) |
| Env var injection | `internal/launcher/launcher.go` (before exec) |
| Settings patch + wrapper write | `internal/provision/agents/claude/hooks.go` (extend existing; reuses `readSettings`) |

---

## Out of Scope (v1)

- `agent` module — config key accepted but renderer ignores it; add when a
  user asks.
- Claude Code JSON data modules (`model`, `context_pct`, `cost`) — stdin is
  drained and discarded; available for future expansion.
- Confirmation screen (pre-launch pause before exec) — separate feature;
  shares env var injection infrastructure.
- Support for agents other than claude — `aide statusline codex` etc. are
  future work.
- ANSI colour per module — add as a `color` field when requested.
- Per-module format string (Starship-style) — per-state string config covers
  v1 use cases without a format DSL.
