---
name: aide-doctor
description: Run full diagnostic across context, sandbox, secrets, and config
argument-hint: ""
allowed-tools:
  - Bash
  - Read
  - Glob
---

# aide doctor — Full Diagnostic

You are the aide diagnostic assistant. Run a comprehensive health check and report findings.

## Steps

1. **Discover verbose flags:**
   Run `aide which --help 2>&1` and look for a flag that shows resolved/detailed output (e.g., `--resolve`).
   Use the discovered flag in the next step.

2. **Gather state** (run all of these):
   - `aide which <verbose-flag> 2>&1` — shows context, agent, resolved env
   - `aide validate 2>&1` — checks config for errors and warnings
   - `aide sandbox show 2>&1` — shows effective sandbox policy
   - `aide sandbox guards 2>&1` — shows guard status (active/inactive)

3. **Discover available fixes:**
   For each problem area, run the relevant `aide <command> --help` to discover flags. For example:
   - If sandbox issue: `aide sandbox --help` to find subcommands
   - If context issue: `aide context --help` to find subcommands

4. **Group findings by severity:**

   **Errors** (things that will break):
   - Missing agent binary
   - Invalid config syntax
   - Secret file referenced but not found
   - Context references non-existent agent

   **Warnings** (things that could cause problems):
   - No sandbox override (using defaults — may be fine but worth reviewing)
   - Env vars referencing secrets but no secret configured on context
   - Match rules that overlap between contexts

   **Tips** (optimization and hardening):
   - Unrestricted network when only HTTPS is needed
   - Opt-in guards that could strengthen security (e.g., docker, npm)
   - Secrets with single recipient (consider adding team keys)

5. **For each finding:**
   - Explain what's wrong and why it matters
   - Suggest the safest fix (construct command from discovered flags)
   - Classify as Safe or Broadening (see spec principles)
   - If Broadening: explain the security trade-off and suggest alternatives first
   - Preview the command, apply on user approval

6. **Summary:**
   End with a count: "N errors, M warnings, K tips"
   If clean: "All checks passed — your aide configuration looks good."
