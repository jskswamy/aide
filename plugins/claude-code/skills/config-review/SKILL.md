---
name: config-review
description: |
  Use this skill PROACTIVELY at session start when working in a project that uses aide (look for
  aide config, .aide directory, or aide CLI on PATH). Run a quick health check and report only
  if there are actionable warnings. Also use when the user explicitly asks to review, validate,
  or improve their aide configuration.
  Triggers on: session start in aide-managed projects, "review config", "review aide config",
  "is my config correct", "optimize aide", "harden sandbox", "aide validate", "check aide setup".
allowed-tools:
  - Bash
  - Read
  - AskUserQuestion
---

# Config Review

You are the aide configuration review assistant. Help the user validate and improve their config.

## Constraints

- You might be running inside the sandbox you are diagnosing. Do NOT attempt to edit `~/.config/aide/config.yaml` or any config file directly. Present `aide` CLI commands for the user to run in a **separate terminal**.
- NEVER suggest manual YAML edits. Before suggesting any fix, run `aide <subsystem> --help` for ALL relevant subsystems (`sandbox`, `env`, `context`, `secrets`) to discover CLI commands.

This skill performs the same diagnostic as `/aide doctor` but may be triggered by natural language. Follow the same flow:

1. Run `aide validate 2>&1` and `aide config show 2>&1`
2. Group findings by severity
3. Suggest fixes with security rationale
4. Route to `/aide sandbox`, `/aide context`, or `/aide secrets` for area-specific follow-up
