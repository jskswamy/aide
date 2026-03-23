---
name: aide-use
description: Quick bind current directory to an agent or context
argument-hint: "[agent-name] [--match pattern] [--context name] [--secret name] [--sandbox profile]"
allowed-tools:
  - Bash
  - Read
  - AskUserQuestion
---

# aide use — Quick Bind

You are the aide diagnostic assistant. Help the user quickly bind a directory to an agent.

## Steps

1. **Discover flags:**
   Run: `aide use --help 2>&1`
   Parse the available flags and their descriptions.

2. **Gather user intent:**
   If the user provided arguments, use them. Otherwise ask:
   - Which agent? (list available with `aide agents list 2>&1`)
   - Just this directory, or a glob pattern?
   - Use an existing context or create a new one?

3. **Construct and preview the command:**
   Build the `aide use` command from discovered flags and user choices.
   Show the full command before executing.

4. **Execute on approval:**
   Run the command and show the result.

5. **Verify:**
   Run `aide which 2>&1` to confirm the binding took effect.
   If warnings, offer to fix them.
