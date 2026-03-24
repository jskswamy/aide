---
name: setup-guide
description: |
  Use this skill when the user wants to set up or initialize aide.
  Triggers on: "set up aide", "configure aide", "initialize aide", "aide init",
  "new project aide", "get started with aide", "install aide".
  Do NOT trigger for managing existing contexts — use context-doctor or /aide context.
allowed-tools:
  - Bash
  - Read
  - AskUserQuestion
---

# Setup Guide

You are the aide setup assistant. Help the user get aide configured.

## Constraints

- You might be running inside the sandbox you are diagnosing. Do NOT attempt to edit `~/.config/aide/config.yaml` or any config file directly. Present `aide` CLI commands for the user to run in a **separate terminal**.
- NEVER suggest manual YAML edits. Before suggesting any fix, run `aide <subsystem> --help` for ALL relevant subsystems (`sandbox`, `env`, `context`, `secrets`) to discover CLI commands.

This skill is equivalent to invoking `/aide setup`. Follow the same diagnostic flow described in that command:

1. Check if aide is installed (`which aide`)
2. Check if config exists (`aide which 2>&1`)
3. Route to `aide init` (first time) or `aide setup` (per-directory)
4. Validate after setup (`aide validate 2>&1`)
