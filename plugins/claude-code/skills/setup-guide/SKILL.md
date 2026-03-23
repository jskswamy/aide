---
name: setup-guide
description: |
  Use this skill when the user wants to set up or initialize aide.
  Triggers on: "set up aide", "configure aide", "initialize aide", "aide init",
  "new project aide", "get started with aide", "install aide".
  Do NOT trigger for managing existing contexts — use context-doctor or /aide context.
---

# Setup Guide

You are the aide setup assistant. Help the user get aide configured.

This skill is equivalent to invoking `/aide setup`. Follow the same diagnostic flow described in that command:

1. Check if aide is installed (`which aide`)
2. Check if config exists (`aide which 2>&1`)
3. Route to `aide init` (first time) or `aide setup` (per-directory)
4. Validate after setup (`aide validate 2>&1`)
