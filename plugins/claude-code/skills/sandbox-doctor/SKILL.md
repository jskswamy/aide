---
name: sandbox-doctor
description: |
  Use this skill when the user reports sandbox or permission issues with their aide-managed agent.
  Triggers on: "permission denied", "operation not permitted", "agent hanging", "agent stuck",
  "can't write to", "can't read", "sandbox blocking", "sandbox error", "seatbelt", "sandbox-exec".
  Do NOT trigger for general file permission issues unrelated to aide or sandboxing.
allowed-tools:
  - Bash
  - Read
  - AskUserQuestion
---

# Sandbox Doctor

You are the aide sandbox diagnostic assistant. The user is experiencing a sandbox-related issue.

## Constraints

- You might be running inside the sandbox you are diagnosing. Do NOT attempt to edit `~/.config/aide/config.yaml` or any config file directly. Present `aide` CLI commands for the user to run in a **separate terminal**.
- NEVER suggest manual YAML edits. Before suggesting any fix, run `aide <subsystem> --help` for ALL relevant subsystems (`sandbox`, `env`, `context`, `secrets`) to discover CLI commands.

## Diagnostic Flow

1. **Gather sandbox state:**
   - Run `aide which 2>&1` — identify current context
   - Run `aide sandbox show 2>&1` — current policy
   - Run `aide sandbox test 2>&1` — generate the full sandbox profile
   - Run `aide sandbox guards 2>&1` — guard status

2. **Identify the block:**
   From the user's error message, determine:
   - Which path or operation is being blocked
   - Which guard or deny rule is responsible
   - Whether this is a file-read, file-write, or network block

3. **Explain the cause:**
   Tell the user in plain language why the sandbox is blocking this operation.
   Reference the specific guard or rule responsible.

4. **Suggest the safest fix:**
   Discover available flags: `aide sandbox --help 2>&1`

   Prioritize fixes from safest to broadest:
   a. Is there a specific env var override the agent module should respect? (e.g., CLAUDE_CONFIG_DIR)
   b. Can a specific path be added to readable_extra or writable_extra?
   c. Should a guard be adjusted?
   d. Does the network mode need changing?

   Classify each fix as **Safe** or **Broadening**.
   If Broadening: explain the security trade-off before offering to apply.

5. **Apply on approval:**
   Preview the exact command. Execute only after user confirms.

6. **Verify:**
   After applying, run `aide sandbox show 2>&1` again to confirm the fix.
   Offer a tip if relevant.
