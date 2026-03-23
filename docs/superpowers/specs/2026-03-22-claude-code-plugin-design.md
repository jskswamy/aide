# Claude Code Plugin for aide

**Date:** 2026-03-22 (updated 2026-03-23)
**Status:** Draft

## Problem

Users should not need to read documentation to use aide. Since LLMs do most of the work, aide should be manageable conversationally through Claude Code's plugin system.

Beyond convenience, the plugin should act as a **diagnostic assistant** — when something goes wrong (sandbox blocking, wrong context, missing API key), it should diagnose the issue, explain what's happening, suggest the safest fix, and offer security/optimization guidance.

## Principles

### Diagnostic-First

Every command and skill follows the same flow:
1. **Observe** — gather state by running aide CLI commands
2. **Diagnose** — explain what's happening and why
3. **Recommend** — suggest the safest fix, with security/optimization tips
4. **Apply** — preview the command, execute on user approval

### Dynamic Command Discovery

The plugin never hardcodes aide CLI **flags or flag values**. Flags and their descriptions are discovered at runtime via `--help`. This means aide CLI is the single source of truth — if a flag gets renamed or added, the plugin adapts without changes.

**What lives in the plugin files (static):**
- Trigger phrases for auto-activation
- Top-level command names used in diagnostic flows (e.g., `aide which`, `aide sandbox show`) — these are stable CLI entry points
- Classification of operations (safe vs. broadening access)
- The *kinds* of tips to look for (security hardening, optimization)

**What comes from aide at runtime (dynamic):**
- Available subcommands and flags (via `aide <command> --help`)
- Flag names, descriptions, and valid values
- Actual command strings shown to users (constructed from discovered flags)

### Safe vs. Broadening Operations

When the plugin constructs a fix, it classifies it:
- **Safe** (e.g., adding a readable path, enabling a guard) — previews command, applies on approval
- **Broadening** (e.g., adding writable access, relaxing network mode, disabling a guard) — explains the security trade-off first, suggests safer alternatives if possible, then offers to apply

### Proactive Tips

After resolving an issue or completing a command, the plugin offers one relevant tip. Tips are constructed dynamically from aide CLI output, never hardcoded. Examples of tip *categories*:
- Security hardening: suggest restricting network to specific ports, enabling opt-in guards
- Optimization: suggest using named sandbox profiles shared across contexts
- Hygiene: suggest running `/aide doctor` periodically, rotating secrets with multiple recipients

## Design

### 1. Plugin Location

Lives at `plugins/claude-code/` in the aide repo. Ships with aide, versioned together to prevent drift between CLI and plugin.

### 2. Interaction Model

Every operation shells out to the `aide` CLI via Bash. The plugin is a diagnostic assistant — it translates user problems into CLI commands, runs them, explains the results, and suggests fixes. No direct file manipulation.

Before suggesting any command, the plugin runs `aide <subcommand> --help` to discover current flags and subcommands. This prevents drift between plugin suggestions and actual CLI capabilities.

### 3. SessionStart Hook

Registered in `.claude-plugin/plugin.json` as a prompt-based SessionStart hook. Runs on every new session:

1. **Check aide availability** — verify `aide` is on PATH. If not, show install guidance and exit.
2. **Run diagnostics quietly** — execute `aide which` and `aide validate`.
3. **Report only if actionable:**
   - Problem detected: `aide: context 'work' has 2 warnings — run /aide doctor to investigate`
   - No context matches: `aide: no context matches this directory — run /aide setup to configure`
   - Everything clean: silent (aide's own startup banner covers status)
4. **Error handling** — if `aide which` or `aide validate` fails (corrupt config, aide crashes), show a brief error with the failing command's stderr and suggest `aide validate` for manual investigation. Never block session start.

The aide CLI already prints a full status banner (context, agent, sandbox policy, blocked/allowed paths, network mode, guards). The hook does not duplicate this — it adds diagnostic value on top.

### 4. User Configuration

File: `.claude/aide-plugin.local.md` in the project directory (follows the standard Claude Code plugin settings pattern). Falls back to `~/.claude/aide-plugin.local.md` for global defaults. YAML frontmatter:

```yaml
---
session_start:
  show_warnings: true    # false to suppress diagnostic hints
  show_tips: true        # false to suppress optimization tips
---
```

### 5. Commands (Slash Commands)

10 commands, all diagnostic-first:

| Command | Purpose |
|---------|---------|
| `/aide` | Quick status — runs `aide which`, shows context + any warnings, routes to other commands |
| `/aide setup` | Guided setup — detects if context exists. Wraps `aide init` (first-time) or `aide setup` (per-directory), offering create/inherit/modify |
| `/aide doctor` | Full diagnostic — runs `aide validate` + `aide sandbox show` + `aide which --resolve` (shows decrypted keys and resolved env), reports issues grouped by severity, suggests fixes for each |
| `/aide sandbox` | Sandbox diagnostics — shows current policy, explains what's blocked and why, helps tune guards/paths/network |
| `/aide context` | Context diagnostics — explains why current context matched, helps add/modify/rename contexts and match rules |
| `/aide secrets` | Secret management — runs `aide secrets list` to show available secret files, `aide secrets keys` to show encryption recipients, helps create/edit/rotate |
| `/aide env` | Environment variable management — runs `aide env list` to show vars for current context, diagnoses missing vars, wires values from secrets via `aide env set --from-secret` |
| `/aide config` | Config review — runs validation, suggests hardening and optimization, offers to open editor |
| `/aide agents` | Agent management — lists agents, checks binaries exist on PATH, helps add/remove/switch |
| `/aide use` | Quick bind — binds current directory (or a glob pattern) to an agent/context. Wraps `aide use` with guided options for match rules, secrets, and sandbox profiles |

Each command discovers available subcommands and flags by running `aide <command> --help` before suggesting any action.

### 6. Skills (Auto-Triggered)

5 skills that activate from natural language — no slash command needed:

| Skill | Trigger Phrases | Diagnostic Flow |
|-------|----------------|-----------------|
| `sandbox-doctor` | "permission denied", "agent hanging", "can't write to", "sandbox blocking", "operation not permitted" | Runs `aide sandbox show` + `aide sandbox test`, identifies blocked path/operation, explains why, suggests safest fix |
| `context-doctor` | "wrong agent", "wrong context", "why is it using", "launched the wrong", "expected claude but got" | Runs `aide which`, explains match resolution order, identifies conflicting rule, offers fix |
| `secrets-doctor` | "can't access API", "missing key", "authentication failed", "API error", "unauthorized" | Runs `aide env list` to check context env vars, `aide secrets list` to show available secret files, traces the missing variable to its source, offers wiring via `aide env set --from-secret` |
| `setup-guide` | "set up aide", "configure aide", "initialize aide", "new project" | Detects current state, runs appropriate setup flow |
| `config-review` | "review config", "is my config correct", "optimize aide", "harden sandbox" | Runs `aide validate`, groups findings by severity, offers actionable fixes with security rationale. Routes to `/aide sandbox`, `/aide context`, etc. for area-specific follow-up |

### 7. File Structure

```
plugins/claude-code/
├── .claude-plugin/
│   └── plugin.json          # Registers commands, skills, hooks
├── commands/
│   ├── aide.md              # /aide — status overview
│   ├── aide-setup.md        # /aide setup — guided setup
│   ├── aide-doctor.md       # /aide doctor — full diagnostic
│   ├── aide-sandbox.md      # /aide sandbox — sandbox diagnostics
│   ├── aide-context.md      # /aide context — context management
│   ├── aide-secrets.md      # /aide secrets — secret management
│   ├── aide-env.md          # /aide env — environment variable management
│   ├── aide-config.md       # /aide config — validation + tips
│   ├── aide-agents.md       # /aide agents — agent management
│   └── aide-use.md          # /aide use — quick bind to agent/context
├── skills/
│   ├── sandbox-doctor/
│   │   └── SKILL.md
│   ├── context-doctor/
│   │   └── SKILL.md
│   ├── secrets-doctor/
│   │   └── SKILL.md
│   ├── setup-guide/
│   │   └── SKILL.md
│   └── config-review/
│       └── SKILL.md
├── hooks/
│   └── session-start.md     # SessionStart hook prompt (registered in plugin.json)
└── aide-plugin.local.md     # User config template
```

The SessionStart hook is a **prompt-based hook** — `session-start.md` contains the hook's prompt, and `.claude-plugin/plugin.json` registers it as a `SessionStart` event handler.

## What This Does NOT Include

- **MCP server** — aide CLI is the interface, no need for a protocol layer.
- **Agents (subagents)** — commands are conversational enough without dedicated subagents.
- **PreToolUse/PostToolUse hooks** — the plugin advises, it does not intercept tool calls.
- **Hardcoded CLI commands** — all commands and flags discovered at runtime via `--help`.
