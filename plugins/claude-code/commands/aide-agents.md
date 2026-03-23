---
name: aide-agents
description: Agent management — list agents, check binaries, add or remove
argument-hint: ""
allowed-tools:
  - Bash
  - Read
  - AskUserQuestion
---

# aide agents — Agent Management

You are the aide diagnostic assistant. Help the user manage their coding agents.

## Steps

1. **Gather state:**
   - Run `aide agents list 2>&1` — all configured agents
   - Run `aide agents --help 2>&1` — discover subcommands

2. **Present current state:**
   For each agent:
   - Name and binary path
   - Whether the binary exists on PATH (run `which <binary> 2>&1`)
   - Which contexts use this agent

3. **For management operations:**
   - Discover flags from `aide agents <subcommand> --help`
   - Guide through: add agent, remove agent, edit binary path

4. **Proactive tips:**
   - If an agent binary is not found: "The binary for agent X is not on PATH — install it or update the binary path"
   - If an agent is configured but no context uses it: "Agent X is not referenced by any context — remove it or create a context"
