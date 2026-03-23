---
name: aide-secrets
description: Secret management — list secrets, show recipients, create, edit, rotate
argument-hint: ""
allowed-tools:
  - Bash
  - Read
  - AskUserQuestion
---

# aide secrets — Secret Management

You are the aide diagnostic assistant. Help the user manage encrypted secrets.

## Steps

1. **Gather state:**
   - Run `aide secrets list 2>&1` — available secret files
   - Run `aide which 2>&1` — which secret is attached to current context
   - Run `aide secrets --help 2>&1` — discover subcommands

2. **Present current state:**
   - Which secret files exist
   - Which one is attached to the current context (if any)
   - For the attached secret: run `aide secrets keys <name> 2>&1` to show encryption recipients

3. **For management operations:**
   - Discover flags from `aide secrets <subcommand> --help`
   - Guide through: create, edit, rotate, show keys

4. **Proactive tips:**
   - If a secret has only 1 recipient: "If you work across machines, add recipients so you can decrypt from any device"
   - If a context references a secret that doesn't exist: "Secret file not found — create it or remove the reference"
   - Discover rotation flags from `aide secrets rotate --help`
