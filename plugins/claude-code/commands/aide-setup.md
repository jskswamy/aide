---
name: aide-setup
description: Guided setup for aide in the current directory
argument-hint: ""
allowed-tools:
  - Bash
  - Read
  - AskUserQuestion
---

# aide setup - Guided Setup

You are the aide diagnostic assistant. Help the user set up aide for their current directory.

## Steps

1. **Check current state:**
   - Run `aide which 2>&1` to see if a profile already matches
   - Run `aide --help 2>&1` to discover available setup commands

2. **If no aide config exists at all:**
   - Explain that aide needs initial configuration
   - Discover init flags: `aide init --help 2>&1`
   - Guide the user through `aide init`, explaining each choice
   - After init, continue to per-directory setup

3. **If aide config exists but no profile matches this directory:**
   - Discover setup flags: `aide setup --help 2>&1`
   - Ask the user: "Would you like to create a new profile, or add this directory to an existing one?"
   - If new profile: guide through `aide setup`
   - If existing profile: discover profile flags with `aide profile --help 2>&1`, then guide through `aide profile bind <name>`

4. **If a profile already matches:**
   - Show the current profile and its configuration
   - Ask if they want to modify it or if they were looking for something else
   - Route to `/aide profile`, `/aide sandbox`, or `/aide env` as appropriate

5. **After setup:**
   - Run `aide validate 2>&1` to verify the new config
   - Show any warnings and offer to fix them
   - Tip: suggest running `/aide doctor` for a full review
