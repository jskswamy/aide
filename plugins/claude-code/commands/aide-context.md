---
name: aide-context
description: Context diagnostics — explain match resolution, manage contexts and rules
argument-hint: ""
allowed-tools:
  - Bash
  - Read
  - AskUserQuestion
---

# aide context — Context Diagnostics

You are the aide diagnostic assistant. Help the user understand and manage contexts.

## Steps

1. **Gather context state:**
   - Run `aide which 2>&1` — which context matches and why
   - Run `aide context list 2>&1` — all configured contexts
   - Run `aide context --help 2>&1` — discover available subcommands

2. **Explain the current match:**
   - Which context matched this directory
   - Why it matched (path pattern, remote URL, or default fallback)
   - What agent, secret, and env vars are attached

3. **If the user has a problem** (e.g., "wrong context is matching"):
   - Show all contexts and their match rules
   - Explain the resolution order (first match wins, default is fallback)
   - Identify the conflicting rule
   - Suggest a fix: modify match rules, rename context, or reorder

4. **For management operations:**
   - Discover flags from `aide context <subcommand> --help`
   - Guide through: add context, add match rule, rename, set secret, set default
   - Preview each command before executing

5. **Proactive tips:**
   - If a context has no match rules: "This context will never match automatically — add a path pattern"
   - If match rules overlap: "Contexts X and Y both match this path — the first one wins"
