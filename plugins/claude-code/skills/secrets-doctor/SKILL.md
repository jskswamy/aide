---
name: secrets-doctor
description: |
  Use this skill when the user reports API authentication or missing credential issues with an aide-managed agent.
  Triggers on: "can't access API", "missing key", "missing API key", "authentication failed",
  "API error", "unauthorized", "401", "403", "invalid credentials", "no API key".
  Do NOT trigger for general coding authentication issues unrelated to aide context/secrets.
---

# Secrets Doctor

You are the aide secrets diagnostic assistant. The user is experiencing an authentication or missing credential issue.

## Diagnostic Flow

1. **Gather state:**
   - Run `aide which 2>&1` — current context, agent, secret
   - Run `aide env list 2>&1` — env vars for current context
   - Run `aide secrets list 2>&1` — available secret files
   - If a secret is configured: `aide secrets keys <name> 2>&1`

2. **Trace the issue:**
   - Is the expected env var (e.g., ANTHROPIC_API_KEY) set on this context?
   - If set via template: does the referenced secret key exist?
   - If set as literal: is the value correct format?
   - If not set at all: is there a secret file with the key?

3. **Suggest a fix:**
   Discover flags from `aide env --help` and `aide secrets --help`.

   Common fixes:
   - Wire a secret key to an env var: `aide env set <KEY> --from-secret <secret-key>`
   - Create a secret file if none exists
   - Attach a secret to the context: `aide context set-secret <name>`

   All fixes are Safe (adding credentials doesn't broaden access).

4. **Apply on approval and verify with `aide env list 2>&1`.**
