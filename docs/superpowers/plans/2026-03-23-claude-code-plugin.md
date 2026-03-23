# Claude Code Plugin for aide — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a Claude Code plugin that provides diagnostic-first slash commands, auto-triggering skills, and a SessionStart hook for the aide CLI.

**Architecture:** The plugin is pure markdown + JSON — no compiled code. Each command/skill is a markdown file with YAML frontmatter. All commands shell out to the `aide` CLI via Bash. Flags are discovered at runtime via `aide <command> --help`, never hardcoded. A SessionStart hook checks for issues on every session.

**Tech Stack:** Claude Code plugin system (markdown commands, skills, hooks), Bash, aide CLI

**Test approach:** Since this is a markdown plugin, testing means: (1) verify the plugin loads without errors, (2) verify each command can be invoked, (3) verify the hook fires. Use `aide --help` output to validate command references are accurate.

---

## File Structure

| File | Responsibility |
|------|---------------|
| `plugins/claude-code/.claude-plugin/plugin.json` | Plugin metadata + hook registration |
| `plugins/claude-code/hooks/hooks.json` | SessionStart hook configuration |
| `plugins/claude-code/hooks/session-start.md` | SessionStart hook prompt |
| `plugins/claude-code/commands/aide.md` | `/aide` — status overview |
| `plugins/claude-code/commands/aide-setup.md` | `/aide setup` — guided setup |
| `plugins/claude-code/commands/aide-doctor.md` | `/aide doctor` — full diagnostic |
| `plugins/claude-code/commands/aide-sandbox.md` | `/aide sandbox` — sandbox diagnostics |
| `plugins/claude-code/commands/aide-context.md` | `/aide context` — context management |
| `plugins/claude-code/commands/aide-secrets.md` | `/aide secrets` — secret management |
| `plugins/claude-code/commands/aide-env.md` | `/aide env` — env var management |
| `plugins/claude-code/commands/aide-config.md` | `/aide config` — config review |
| `plugins/claude-code/commands/aide-agents.md` | `/aide agents` — agent management |
| `plugins/claude-code/commands/aide-use.md` | `/aide use` — quick bind |
| `plugins/claude-code/skills/sandbox-doctor/SKILL.md` | Auto-trigger on sandbox issues |
| `plugins/claude-code/skills/context-doctor/SKILL.md` | Auto-trigger on context issues |
| `plugins/claude-code/skills/secrets-doctor/SKILL.md` | Auto-trigger on auth/key issues |
| `plugins/claude-code/skills/setup-guide/SKILL.md` | Auto-trigger on setup requests |
| `plugins/claude-code/skills/config-review/SKILL.md` | Auto-trigger on config review |
| `plugins/claude-code/aide-plugin.local.md` | User config template |

---

### Task 1: Plugin scaffold — plugin.json, hooks.json, SessionStart hook

**Files:**
- Create: `plugins/claude-code/.claude-plugin/plugin.json`
- Create: `plugins/claude-code/hooks/hooks.json`
- Create: `plugins/claude-code/hooks/session-start.md`
- Create: `plugins/claude-code/aide-plugin.local.md`

- [ ] **Step 1: Create directory structure**

```bash
mkdir -p plugins/claude-code/.claude-plugin
mkdir -p plugins/claude-code/hooks
mkdir -p plugins/claude-code/commands
mkdir -p plugins/claude-code/skills
```

- [ ] **Step 2: Create plugin.json**

Create `plugins/claude-code/.claude-plugin/plugin.json`:

```json
{
  "name": "aide",
  "version": "0.1.0",
  "description": "Diagnostic assistant for the aide CLI — manages contexts, sandbox, secrets, and agents conversationally",
  "author": {
    "name": "jskswamy",
    "email": "jskswamy@users.noreply.github.com"
  },
  "keywords": ["aide", "sandbox", "context", "secrets", "agents", "diagnostic"]
}
```

- [ ] **Step 3: Create hooks.json**

Create `plugins/claude-code/hooks/hooks.json`:

```json
{
  "description": "aide plugin hooks — SessionStart diagnostic check",
  "hooks": {
    "SessionStart": [
      {
        "matcher": "*",
        "hooks": [
          {
            "type": "prompt",
            "prompt": "${CLAUDE_PLUGIN_ROOT}/hooks/session-start.md"
          }
        ]
      }
    ]
  }
}
```

- [ ] **Step 4: Create session-start.md hook prompt**

Create `plugins/claude-code/hooks/session-start.md`:

```markdown
You are the aide diagnostic assistant. On session start, perform a quick health check.

## Steps

1. **Check aide is available:**
   Run: `which aide`
   If not found, output:
   > aide is not installed or not on PATH. Visit the aide repo for installation instructions.
   Then stop — do not run further checks.

2. **Check current context:**
   Run: `aide which 2>&1`
   Note the exit code. If it fails, this directory has no matching context.

3. **Run validation:**
   Run: `aide validate 2>&1`
   Count any warnings or errors in the output.

4. **Read user preferences:**
   Check if `.claude/aide-plugin.local.md` exists in the project directory.
   If it exists, read the YAML frontmatter for `session_start.show_warnings` and `session_start.show_tips`.
   Defaults: show_warnings=true, show_tips=true.

5. **Report only if actionable (and show_warnings is true):**
   - If `aide which` failed (no context matches): output a single line:
     > aide: no context matches this directory — run `/aide setup` to configure
   - If `aide validate` found warnings/errors: output a single line:
     > aide: N warning(s) found — run `/aide doctor` to investigate
   - If both pass cleanly: output nothing. The aide CLI's own startup banner already shows context status.

6. **Error handling:**
   If `aide which` or `aide validate` crashes (non-zero exit with unexpected output), show:
   > aide: health check failed — run `aide validate` manually to investigate
   Never block the session. Always let the user continue.

## Important
- Do NOT duplicate the aide startup banner (context, agent, sandbox info) — aide already shows this.
- Keep output to one line maximum. This runs on every session start.
- If show_warnings is false in user config, skip all output.
```

- [ ] **Step 5: Create user config template**

Create `plugins/claude-code/aide-plugin.local.md`:

```markdown
---
session_start:
  show_warnings: true
  show_tips: true
---

# aide Plugin Settings

Configure the aide Claude Code plugin behavior.

## Settings

| Key | Default | Description |
|-----|---------|-------------|
| session_start.show_warnings | true | Show diagnostic warnings on session start |
| session_start.show_tips | true | Show optimization tips after commands |
```

- [ ] **Step 6: Commit**

```bash
git add plugins/claude-code/
git commit -m "Add aide plugin scaffold with SessionStart hook"
```

---

### Task 2: Core commands — `/aide` and `/aide doctor`

**Files:**
- Create: `plugins/claude-code/commands/aide.md`
- Create: `plugins/claude-code/commands/aide-doctor.md`

- [ ] **Step 1: Create `/aide` command**

Create `plugins/claude-code/commands/aide.md`:

```markdown
---
name: aide
description: Show current aide context status and route to diagnostic commands
argument-hint: ""
allowed-tools:
  - Bash
  - Read
  - Glob
---

# aide — Status Overview

You are the aide diagnostic assistant. Show the current aide context status and help the user navigate to the right command.

## Steps

1. **Check aide is available:**
   Run: `aide which 2>&1`
   If aide is not found, explain how to install it and stop.

2. **Show context status:**
   Present the output of `aide which` clearly. Explain:
   - Which context matched and why (path match, remote match, or default)
   - Which agent is configured
   - Whether a secret is attached
   - What environment variables are set

3. **Discover available commands:**
   Run: `aide --help 2>&1`
   Use the output to understand what aide can do. Do NOT hardcode command lists.

4. **Offer navigation:**
   Based on the status, suggest relevant next steps:
   - If no context matches: suggest `/aide setup`
   - If warnings visible: suggest `/aide doctor`
   - Otherwise: list available `/aide` subcommands the user can try

5. **Proactive tip (if show_tips is true in `.claude/aide-plugin.local.md`):**
   Offer one relevant tip based on what you observed. Examples:
   - If sandbox is using defaults: "Tip: review your sandbox policy with `/aide sandbox` to ensure it matches your needs"
   - If no secret is configured: "Tip: wire API keys with `/aide secrets` for secure credential management"
```

- [ ] **Step 2: Create `/aide doctor` command**

Create `plugins/claude-code/commands/aide-doctor.md`:

```markdown
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
```

- [ ] **Step 3: Commit**

```bash
git add plugins/claude-code/commands/aide.md plugins/claude-code/commands/aide-doctor.md
git commit -m "Add /aide and /aide doctor commands"
```

---

### Task 3: Setup and bind commands — `/aide setup` and `/aide use`

**Files:**
- Create: `plugins/claude-code/commands/aide-setup.md`
- Create: `plugins/claude-code/commands/aide-use.md`

- [ ] **Step 1: Create `/aide setup` command**

Create `plugins/claude-code/commands/aide-setup.md`:

```markdown
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
```

- [ ] **Step 2: Create `/aide use` command**

Create `plugins/claude-code/commands/aide-use.md`:

```markdown
---
name: aide-use
description: Quick bind current directory to an agent or context
argument-hint: "[agent-name] [--match pattern] [--context name] [--secret name] [--sandbox profile]"
allowed-tools:
  - Bash
  - Read
  - AskUserQuestion
---

# aide use — Quick Bind

You are the aide diagnostic assistant. Help the user quickly bind a directory to an agent.

## Steps

1. **Discover flags:**
   Run: `aide use --help 2>&1`
   Parse the available flags and their descriptions.

2. **Gather user intent:**
   If the user provided arguments, use them. Otherwise ask:
   - Which agent? (list available with `aide agents list 2>&1`)
   - Just this directory, or a glob pattern?
   - Use an existing context or create a new one?

3. **Construct and preview the command:**
   Build the `aide use` command from discovered flags and user choices.
   Show the full command before executing.

4. **Execute on approval:**
   Run the command and show the result.

5. **Verify:**
   Run `aide which 2>&1` to confirm the binding took effect.
   If warnings, offer to fix them.
```

- [ ] **Step 3: Commit**

```bash
git add plugins/claude-code/commands/aide-setup.md plugins/claude-code/commands/aide-use.md
git commit -m "Add /aide setup and /aide use commands"
```

---

### Task 4: Sandbox and context commands

**Files:**
- Create: `plugins/claude-code/commands/aide-sandbox.md`
- Create: `plugins/claude-code/commands/aide-context.md`

- [ ] **Step 1: Create `/aide sandbox` command**

Create `plugins/claude-code/commands/aide-sandbox.md`:

```markdown
---
name: aide-sandbox
description: Sandbox diagnostics — show policy, explain blocks, tune guards and paths
argument-hint: ""
allowed-tools:
  - Bash
  - Read
  - AskUserQuestion
---

# aide sandbox — Sandbox Diagnostics

You are the aide diagnostic assistant. Help the user understand and manage their sandbox policy.

## Steps

1. **Gather sandbox state:**
   - Run `aide sandbox show 2>&1` — current policy for this context
   - Run `aide sandbox guards 2>&1` — all guards with status
   - Run `aide sandbox --help 2>&1` — discover available subcommands

2. **Present the current policy clearly:**
   Explain in plain language:
   - Network mode (outbound/none/unrestricted) and what it means
   - Which guards are active and what each one blocks
   - Which paths are blocked (denied) and why
   - Which extra paths have been granted (writable/readable)

3. **If the user describes a specific problem** (e.g., "can't write to X"):
   - Identify which guard or deny rule is blocking it
   - Explain why that rule exists (security purpose)
   - Suggest the **safest** fix:
     - If a specific path: use the narrowest permission (readable before writable)
     - If a guard is too restrictive: suggest unguarding that specific guard rather than broadening everything
   - Classify the fix as Safe or Broadening
   - If Broadening: explain the trade-off, suggest alternatives

4. **If the user wants to explore:**
   - Show what subcommands are available (from --help)
   - Offer guided walkthroughs: "tune network", "manage guards", "add paths"

5. **Proactive tips:**
   - If network is unrestricted: "Consider restricting to outbound-only or specific ports if your agent only needs HTTPS"
   - If an opt-in guard could help: "The docker guard blocks access to Docker credentials — enable it with `aide sandbox guard docker` if you use containers"
   - Discover these tips from `aide sandbox types 2>&1` output
```

- [ ] **Step 2: Create `/aide context` command**

Create `plugins/claude-code/commands/aide-context.md`:

```markdown
---
name: aide-context
description: Context diagnostics — explain match resolution, manage contexts and rules
argument-hint: ""
allowed-tools:
  - Bash
  - Read
  - AskUserQuestion
---

# aide context — Context Diagnostics

You are the aide diagnostic assistant. Help the user understand and manage contexts.

## Steps

1. **Gather context state:**
   - Run `aide which 2>&1` — which context matches and why
   - Run `aide context list 2>&1` — all configured contexts
   - Run `aide context --help 2>&1` — discover available subcommands

2. **Explain the current match:**
   - Which context matched this directory
   - Why it matched (path pattern, remote URL, or default fallback)
   - What agent, secret, and env vars are attached

3. **If the user has a problem** (e.g., "wrong context is matching"):
   - Show all contexts and their match rules
   - Explain the resolution order (first match wins, default is fallback)
   - Identify the conflicting rule
   - Suggest a fix: modify match rules, rename context, or reorder

4. **For management operations:**
   - Discover flags from `aide context <subcommand> --help`
   - Guide through: add context, add match rule, rename, set secret, set default
   - Preview each command before executing

5. **Proactive tips:**
   - If a context has no match rules: "This context will never match automatically — add a path pattern"
   - If match rules overlap: "Contexts X and Y both match this path — the first one wins"
```

- [ ] **Step 3: Commit**

```bash
git add plugins/claude-code/commands/aide-sandbox.md plugins/claude-code/commands/aide-context.md
git commit -m "Add /aide sandbox and /aide context commands"
```

---

### Task 5: Secrets, env, config, and agents commands

**Files:**
- Create: `plugins/claude-code/commands/aide-secrets.md`
- Create: `plugins/claude-code/commands/aide-env.md`
- Create: `plugins/claude-code/commands/aide-config.md`
- Create: `plugins/claude-code/commands/aide-agents.md`

- [ ] **Step 1: Create `/aide secrets` command**

Create `plugins/claude-code/commands/aide-secrets.md`:

```markdown
---
name: aide-secrets
description: Secret management — list secrets, show recipients, create, edit, rotate
argument-hint: ""
allowed-tools:
  - Bash
  - Read
  - AskUserQuestion
---

# aide secrets — Secret Management

You are the aide diagnostic assistant. Help the user manage encrypted secrets.

## Steps

1. **Gather state:**
   - Run `aide secrets list 2>&1` — available secret files
   - Run `aide which 2>&1` — which secret is attached to current context
   - Run `aide secrets --help 2>&1` — discover subcommands

2. **Present current state:**
   - Which secret files exist
   - Which one is attached to the current context (if any)
   - For the attached secret: run `aide secrets keys <name> 2>&1` to show encryption recipients

3. **For management operations:**
   - Discover flags from `aide secrets <subcommand> --help`
   - Guide through: create, edit, rotate, show keys

4. **Proactive tips:**
   - If a secret has only 1 recipient: "If you work across machines, add recipients so you can decrypt from any device"
   - If a context references a secret that doesn't exist: "Secret file not found — create it or remove the reference"
   - Discover rotation flags from `aide secrets rotate --help`
```

- [ ] **Step 2: Create `/aide env` command**

Create `plugins/claude-code/commands/aide-env.md`:

```markdown
---
name: aide-env
description: Environment variable management — list, set, wire from secrets
argument-hint: ""
allowed-tools:
  - Bash
  - Read
  - AskUserQuestion
---

# aide env — Environment Variable Management

You are the aide diagnostic assistant. Help the user manage environment variables on their context.

## Steps

1. **Gather state:**
   - Run `aide env list 2>&1` — env vars for current context
   - Run `aide which 2>&1` — current context details
   - Run `aide env --help 2>&1` — discover subcommands and flags

2. **Present current state:**
   - List all env vars set on the current context
   - Note which use template syntax (e.g., `{{ .secrets.key }}`) vs. literal values
   - Flag any template references to secrets when no secret is configured

3. **For setting variables:**
   - Discover flags from `aide env set --help`
   - Ask: literal value or from secrets?
   - If from secrets: discover available keys with `aide secrets keys <name> 2>&1`, offer interactive selection
   - Preview the command, execute on approval

4. **Proactive tips:**
   - If a literal API key is set: "Consider storing this in secrets instead — literal values are visible in the config file"
   - If env var references a secret key that doesn't exist: "This template will fail at launch — verify the key name"
```

- [ ] **Step 3: Create `/aide config` command**

Create `plugins/claude-code/commands/aide-config.md`:

```markdown
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
```

- [ ] **Step 4: Create `/aide agents` command**

Create `plugins/claude-code/commands/aide-agents.md`:

```markdown
---
name: aide-agents
description: Agent management — list agents, check binaries, add or remove
argument-hint: ""
allowed-tools:
  - Bash
  - Read
  - AskUserQuestion
---

# aide agents — Agent Management

You are the aide diagnostic assistant. Help the user manage their coding agents.

## Steps

1. **Gather state:**
   - Run `aide agents list 2>&1` — all configured agents
   - Run `aide agents --help 2>&1` — discover subcommands

2. **Present current state:**
   For each agent:
   - Name and binary path
   - Whether the binary exists on PATH (run `which <binary> 2>&1`)
   - Which contexts use this agent

3. **For management operations:**
   - Discover flags from `aide agents <subcommand> --help`
   - Guide through: add agent, remove agent, edit binary path

4. **Proactive tips:**
   - If an agent binary is not found: "The binary for agent X is not on PATH — install it or update the binary path"
   - If an agent is configured but no context uses it: "Agent X is not referenced by any context — remove it or create a context"
```

- [ ] **Step 5: Commit**

```bash
git add plugins/claude-code/commands/aide-secrets.md plugins/claude-code/commands/aide-env.md plugins/claude-code/commands/aide-config.md plugins/claude-code/commands/aide-agents.md
git commit -m "Add /aide secrets, env, config, and agents commands"
```

---

### Task 6: Auto-triggering skills

**Files:**
- Create: `plugins/claude-code/skills/sandbox-doctor/SKILL.md`
- Create: `plugins/claude-code/skills/context-doctor/SKILL.md`
- Create: `plugins/claude-code/skills/secrets-doctor/SKILL.md`
- Create: `plugins/claude-code/skills/setup-guide/SKILL.md`
- Create: `plugins/claude-code/skills/config-review/SKILL.md`

- [ ] **Step 1: Create sandbox-doctor skill**

Create `plugins/claude-code/skills/sandbox-doctor/SKILL.md`:

```markdown
---
name: sandbox-doctor
description: |
  Use this skill when the user reports sandbox or permission issues with their aide-managed agent.
  Triggers on: "permission denied", "operation not permitted", "agent hanging", "agent stuck",
  "can't write to", "can't read", "sandbox blocking", "sandbox error", "seatbelt", "sandbox-exec".
  Do NOT trigger for general file permission issues unrelated to aide or sandboxing.
---

# Sandbox Doctor

You are the aide sandbox diagnostic assistant. The user is experiencing a sandbox-related issue.

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
```

- [ ] **Step 2: Create context-doctor skill**

Create `plugins/claude-code/skills/context-doctor/SKILL.md`:

```markdown
---
name: context-doctor
description: |
  Use this skill when the user reports context or agent selection issues with aide.
  Triggers on: "wrong agent", "wrong context", "why is it using", "launched the wrong agent",
  "expected claude but got", "context mismatch", "aide which", "which context".
  Do NOT trigger for general agent setup — use setup-guide for that.
---

# Context Doctor

You are the aide context diagnostic assistant. The user is experiencing a context resolution issue.

## Diagnostic Flow

1. **Gather context state:**
   - Run `aide which 2>&1` — which context matched and why
   - Run `aide context list 2>&1` — all contexts with match rules
   - Run `aide context --help 2>&1` — discover subcommands

2. **Explain the match:**
   - Show which context matched and which match rule triggered
   - If unexpected: show all contexts that could match this directory
   - Explain resolution order (first path match wins, default is fallback)

3. **Identify the problem:**
   - Overlapping match rules between contexts
   - Default context being used when a specific one was expected
   - Missing match rule for this directory

4. **Suggest a fix:**
   Discover flags from `aide context --help`.
   Offer the most targeted fix:
   - Add a match rule to the right context
   - Reorder or narrow existing match rules
   - Set a different default context

5. **Apply on approval and verify with `aide which 2>&1`.**
```

- [ ] **Step 3: Create secrets-doctor skill**

Create `plugins/claude-code/skills/secrets-doctor/SKILL.md`:

```markdown
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
```

- [ ] **Step 4: Create setup-guide skill**

Create `plugins/claude-code/skills/setup-guide/SKILL.md`:

```markdown
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
```

- [ ] **Step 5: Create config-review skill**

Create `plugins/claude-code/skills/config-review/SKILL.md`:

```markdown
---
name: config-review
description: |
  Use this skill when the user wants to review, validate, or improve their aide configuration.
  Triggers on: "review config", "review aide config", "is my config correct",
  "optimize aide", "harden sandbox", "aide validate", "check aide setup".
  Routes to /aide doctor for full diagnostic or /aide sandbox, /aide context for specific areas.
---

# Config Review

You are the aide configuration review assistant. Help the user validate and improve their config.

This skill performs the same diagnostic as `/aide doctor` but may be triggered by natural language. Follow the same flow:

1. Run `aide validate 2>&1` and `aide config show 2>&1`
2. Group findings by severity
3. Suggest fixes with security rationale
4. Route to `/aide sandbox`, `/aide context`, or `/aide secrets` for area-specific follow-up
```

- [ ] **Step 6: Create skill directories and commit**

```bash
mkdir -p plugins/claude-code/skills/sandbox-doctor
mkdir -p plugins/claude-code/skills/context-doctor
mkdir -p plugins/claude-code/skills/secrets-doctor
mkdir -p plugins/claude-code/skills/setup-guide
mkdir -p plugins/claude-code/skills/config-review
# (files already created above)
git add plugins/claude-code/skills/
git commit -m "Add auto-triggering diagnostic skills"
```

---

### Task 7: Verify plugin structure

- [ ] **Step 1: Verify all files exist**

```bash
find plugins/claude-code/ -type f | sort
```

Expected output (19 files):
```
plugins/claude-code/.claude-plugin/plugin.json
plugins/claude-code/aide-plugin.local.md
plugins/claude-code/commands/aide-agents.md
plugins/claude-code/commands/aide-config.md
plugins/claude-code/commands/aide-context.md
plugins/claude-code/commands/aide-doctor.md
plugins/claude-code/commands/aide-env.md
plugins/claude-code/commands/aide-sandbox.md
plugins/claude-code/commands/aide-secrets.md
plugins/claude-code/commands/aide-setup.md
plugins/claude-code/commands/aide-use.md
plugins/claude-code/commands/aide.md
plugins/claude-code/hooks/hooks.json
plugins/claude-code/hooks/session-start.md
plugins/claude-code/skills/config-review/SKILL.md
plugins/claude-code/skills/context-doctor/SKILL.md
plugins/claude-code/skills/sandbox-doctor/SKILL.md
plugins/claude-code/skills/secrets-doctor/SKILL.md
plugins/claude-code/skills/setup-guide/SKILL.md
```

- [ ] **Step 2: Validate plugin.json is valid JSON**

```bash
python3 -c "import json; json.load(open('plugins/claude-code/.claude-plugin/plugin.json'))" && echo "valid JSON"
```

- [ ] **Step 3: Validate hooks.json is valid JSON**

```bash
python3 -c "import json; json.load(open('plugins/claude-code/hooks/hooks.json'))" && echo "valid JSON"
```

- [ ] **Step 4: Verify all commands have valid frontmatter**

```bash
for f in plugins/claude-code/commands/*.md; do
  echo "--- $f ---"
  head -3 "$f"
done
```

Each should start with `---` on line 1.

- [ ] **Step 5: Verify all skills have valid frontmatter**

```bash
for f in plugins/claude-code/skills/*/SKILL.md; do
  echo "--- $f ---"
  head -3 "$f"
done
```

Each should start with `---` on line 1.
