# Claude Code Plugin for aide

**Date:** 2026-03-22
**Status:** Draft

## Problem

Users should not need to read documentation to use aide. Since LLMs do most of the work, aide should be manageable conversationally through Claude Code's plugin system.

## Design

### 1. Plugin Location

Lives at `plugins/claude-code/` in the aide repo. Ships with aide, versioned together to prevent drift between CLI and plugin.

### 2. Interaction Model

Every operation shells out to the `aide` CLI via Bash. The plugin is a conversational wrapper — it translates user intent into CLI commands, runs them, and presents results. No direct file manipulation.

### 3. Commands (Slash Commands)

| Command | Wraps | Purpose |
|---------|-------|---------|
| `/aide` | `aide --help` | Show available operations, route to subcommands |
| `/aide init` | `aide init` | First-time setup wizard |
| `/aide context` | `aide context add/list`, `aide which` | Manage contexts |
| `/aide env` | `aide env set/unset/list` | Manage environment variables |
| `/aide secrets` | `aide secrets create/edit/rotate/keys` | Manage encrypted secrets |
| `/aide sandbox` | `aide sandbox show/allow/deny/network/ports` | Manage sandbox policy |
| `/aide agents` | `aide agents list/add/use` | Manage agents |
| `/aide config` | `aide config show`, `aide validate` | View and validate config |

### 4. Skills (Auto-Activation)

| Skill | Trigger Phrases | Action |
|-------|----------------|--------|
| setup | "set up aide", "configure aide", "initialize aide" | Invokes init workflow |
| context-management | "add context", "new context", "switch context", "which context" | Invokes context workflow |
| sandbox-management | "change sandbox", "allow path", "deny path", "sandbox permissions" | Invokes sandbox workflow |
| agent-management | "add agent", "switch agent", "use claude", "use gemini" | Invokes agents workflow |

### 5. Conversational Pattern

Each command follows:
1. **Gather** — Ask questions one at a time to understand what the user wants
2. **Preview** — Show the CLI command that will be executed
3. **Execute** — Run via Bash tool
4. **Confirm** — Show result, offer follow-up actions

### 6. File Structure

```
plugins/claude-code/
├── .claude-plugin/
│   └── plugin.json
├── commands/
│   ├── aide.md
│   ├── aide-init.md
│   ├── aide-context.md
│   ├── aide-env.md
│   ├── aide-secrets.md
│   ├── aide-sandbox.md
│   ├── aide-agents.md
│   └── aide-config.md
├── skills/
│   ├── setup/
│   │   └── SKILL.md
│   ├── context-management/
│   │   └── SKILL.md
│   ├── sandbox-management/
│   │   └── SKILL.md
│   └── agent-management/
│       └── SKILL.md
└── README.md
```

## What This Does NOT Include

- MCP server integration — aide CLI is sufficient, no need for a protocol server.
- Hooks — no tool interception needed for aide operations.
- Agents — commands are simple enough to not need dedicated subagents.
