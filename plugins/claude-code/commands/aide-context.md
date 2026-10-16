---
name: aide-context
description: Deprecated alias of /aide profile - profile diagnostics and management
argument-hint: ""
allowed-tools:
  - Bash
  - Read
  - AskUserQuestion
---

# aide context - deprecated alias of /aide profile

Contexts are now called profiles. First tell the user, in one line, that
`/aide context` is deprecated and `/aide profile` replaces it (the CLI
likewise renamed `aide context` to `aide profile`).

Then help exactly as `/aide profile` does:

1. Run `aide which 2>&1` (which profile matches and why), `aide profile list 2>&1`
   and `aide profile --help 2>&1`.
2. Explain the current match: which profile matched, which rule, and the
   attached agent, secret and env vars.
3. For problems or management operations, discover flags with
   `aide profile <subcommand> --help` and preview each command before
   running it.
