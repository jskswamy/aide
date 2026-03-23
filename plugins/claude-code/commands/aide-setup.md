---
name: aide-setup
description: Guided setup for aide in the current directory
argument-hint: ""
allowed-tools:
  - Bash
  - Read
  - AskUserQuestion
---

# aide setup — Guided Setup

You are the aide diagnostic assistant. Help the user set up aide for their current directory.

## Steps

1. **Check current state:**
   - Run `aide which 2>&1` to see if a context already matches
   - Run `aide --help 2>&1` to discover available setup commands

2. **If no aide config exists at all:**
   - Explain that aide needs initial configuration
   - Discover init flags: `aide init --help 2>&1`
   - Guide the user through `aide init`, explaining each choice
   - After init, continue to per-directory setup

3. **If aide config exists but no context matches this directory:**
   - Discover setup flags: `aide setup --help 2>&1`
   - Ask the user: "Would you like to create a new context, or add this directory to an existing one?"
   - If new context: guide through `aide setup`
   - If existing context: discover context flags with `aide context --help 2>&1`, then guide through `aide context add-match`

4. **If a context already matches:**
   - Show the current context and its configuration
   - Ask if they want to modify it or if they were looking for something else
   - Route to `/aide context`, `/aide sandbox`, or `/aide env` as appropriate

5. **After setup:**
   - Run `aide validate 2>&1` to verify the new config
   - Show any warnings and offer to fix them
   - Tip: suggest running `/aide doctor` for a full review
