## aide — Claude Code Plugin

A diagnostic assistant for the aide CLI. It diagnoses sandbox issues, context mismatches, and missing credentials by inspecting your local configuration and runtime state. When problems are found, it suggests the safest possible fix first and clearly explains security trade-offs for any broadening change, so you always know what you are allowing before you allow it.

### Installation

```bash
# Add the aide repo as a marketplace
/plugin marketplace add jskswamy/aide

# Install the plugin
/plugin install aide@jskswamy-aide

# Reload in active session
/reload-plugins
```

### Team Setup

For projects using aide, add to `.claude/settings.json`:

```json
{
  "extraKnownMarketplaces": {
    "aide-plugins": {
      "source": {
        "source": "github",
        "repo": "jskswamy/aide"
      }
    }
  }
}
```

### Commands

| Command | Purpose |
|---------|---------|
| `/aide` | Quick status — shows current context, warnings, routes to other commands |
| `/aide setup` | Guided setup — wraps `aide init` or `aide setup` |
| `/aide doctor` | Full diagnostic — validates config, sandbox, context, secrets |
| `/aide sandbox` | Sandbox diagnostics — policy review, guard management, path tuning |
| `/aide context` | Context management — match resolution, add/modify/rename contexts |
| `/aide secrets` | Secret management — list, create, edit, rotate encrypted secrets |
| `/aide env` | Environment variables — list, set, wire from secrets |
| `/aide config` | Config review — validation, hardening suggestions, optimization tips |
| `/aide agents` | Agent management — list, add, remove, check binary availability |
| `/aide use` | Quick bind — bind directory to agent/context with guided options |

### Auto-Triggering Skills

| Skill | Activates When |
|-------|---------------|
| sandbox-doctor | "permission denied", "agent hanging", "can't write to" |
| context-doctor | "wrong agent", "wrong context", "why is it using" |
| secrets-doctor | "can't access API", "missing key", "authentication failed" |
| setup-guide | "set up aide", "configure aide", "initialize aide" |
| config-review | "review config", "is my config correct", "optimize aide" |

### Configuration

User preferences in `.claude/aide-plugin.local.md`:

```yaml
---
session_start:
  show_warnings: true
  show_tips: true
---
```

### How It Works

- Every command follows: Observe, Diagnose, Recommend, Apply
- Flags and subcommands are discovered at runtime via `aide --help` (never hardcoded)
- Fixes are classified as Safe or Broadening (security trade-offs explained for broadening changes)
- Proactive tips offered after each interaction
