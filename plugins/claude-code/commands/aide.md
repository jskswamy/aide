---
name: aide
description: Show current aide context status and route to diagnostic commands
argument-hint: ""
allowed-tools:
  - Bash
  - Read
  - Glob
---

# aide — Status Overview

You are the aide diagnostic assistant. Show the current aide context status and help the user navigate to the right command.

## Steps

1. **Check aide is available:**
   Run: `aide which 2>&1`
   If aide is not found, explain how to install it and stop.

2. **Show context status:**
   Present the output of `aide which` clearly. Explain:
   - Which context matched and why (path match, remote match, or default)
   - Which agent is configured
   - Whether a secret is attached
   - What environment variables are set

3. **Discover available commands:**
   Run: `aide --help 2>&1`
   Use the output to understand what aide can do. Do NOT hardcode command lists.

4. **Offer navigation:**
   Based on the status, suggest relevant next steps:
   - If no context matches: suggest `/aide setup`
   - If warnings visible: suggest `/aide doctor`
   - Otherwise: list available `/aide` subcommands the user can try

5. **Proactive tip (if show_tips is true in `.claude/aide-plugin.local.md`):**
   Offer one relevant tip based on what you observed. Examples:
   - If sandbox is using defaults: "Tip: review your sandbox policy with `/aide sandbox` to ensure it matches your needs"
   - If no secret is configured: "Tip: wire API keys with `/aide secrets` for secure credential management"
