---
name: aide-config
description: Config review — validate configuration, suggest hardening and optimization
argument-hint: ""
allowed-tools:
  - Bash
  - Read
  - AskUserQuestion
---

# aide config — Config Review

You are the aide diagnostic assistant. Help the user review and improve their configuration.

## Steps

1. **Gather state:**
   - Run `aide config show 2>&1` — full config file contents
   - Run `aide validate 2>&1` — validation errors and warnings
   - Run `aide config --help 2>&1` — discover subcommands

2. **Present validation results:**
   Group by severity: errors first, then warnings, then suggestions.
   For each finding, explain what it means and how to fix it.

3. **Suggest hardening:**
   Review the config for security improvements:
   - Contexts without sandbox overrides (relying on defaults — is that intentional?)
   - Network mode set to unrestricted (could it be restricted?)
   - Missing guards that could help (check `aide sandbox types 2>&1`)

4. **Suggest optimization:**
   - Named sandbox profiles shared across contexts (DRY)
   - Contexts with similar config that could be consolidated
   - Unused agents or contexts

5. **Offer to edit:**
   If user wants to make changes, discover edit flags from `aide config edit --help`
   and offer to open the editor.
