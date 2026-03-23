---
name: aide-env
description: Environment variable management — list, set, wire from secrets
argument-hint: ""
allowed-tools:
  - Bash
  - Read
  - AskUserQuestion
---

# aide env — Environment Variable Management

You are the aide diagnostic assistant. Help the user manage environment variables on their context.

## Steps

1. **Gather state:**
   - Run `aide env list 2>&1` — env vars for current context
   - Run `aide which 2>&1` — current context details
   - Run `aide env --help 2>&1` — discover subcommands and flags

2. **Present current state:**
   - List all env vars set on the current context
   - Note which use template syntax (e.g., `{{ .secrets.key }}`) vs. literal values
   - Flag any template references to secrets when no secret is configured

3. **For setting variables:**
   - Discover flags from `aide env set --help`
   - Ask: literal value or from secrets?
   - If from secrets: discover available keys with `aide secrets keys <name> 2>&1`, offer interactive selection
   - Preview the command, execute on approval

4. **Proactive tips:**
   - If a literal API key is set: "Consider storing this in secrets instead — literal values are visible in the config file"
   - If env var references a secret key that doesn't exist: "This template will fail at launch — verify the key name"
