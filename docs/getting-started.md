# Getting Started with aide

aide picks the right AI coding agent and credentials for each project directory.
Run `aide` in any project and it resolves which agent to launch and which secrets to use.

## Zero Config

If one agent is on PATH with its API key in the environment, `aide` launches it without a config file.

```
$ aide
Detected agent: claude (/usr/local/bin/claude)
No config file found. Launching with environment credentials.
```

If multiple agents are on PATH, aide cannot pick automatically. Use `--agent`:

```
$ aide --agent codex
```

To skip the agent's built-in permission prompts while keeping the OS sandbox active, add `--auto-approve`:

```
$ aide --auto-approve
```

This injects the agent-specific skip flag (e.g. `--dangerously-skip-permissions` for Claude). `--yolo` is kept as an alias. You can also set `auto_approve: true` (or `yolo: true`) in config for specific contexts. Use `--no-auto-approve` (or `--no-yolo`) to override config-level settings.

## First-Time Setup

`aide setup` runs a guided wizard for the current directory. It detects agents on PATH, optionally creates an encrypted secrets file, and writes a context to `~/.config/aide/config.yaml`.

```
$ cd ~/work/myproject
$ aide setup

Setting up aide for /home/user/work/myproject

Detected agents on PATH: claude, codex, copilot
Agent (default: claude):
Context name (default: myproject):
Match rule (path glob or remote URL)?
  [1] This folder (/home/user/work/myproject)
  [2] A folder path or pattern
  [3] By git repository URL
Select [1]:

Set up secrets? (y/N): y
Age public key: age1abc...
Secrets file name (e.g. personal): work

Created secrets/work.enc.yaml
Created /home/user/.config/aide/config.yaml
```

The wizard asks for: the agent binary to launch, a context name (defaults to the directory name), a match rule (path or remote pattern), and optionally an age public key and secrets file name.

## Using Capabilities

Capabilities grant the sandbox additional permissions (filesystem paths, environment variables, network access) for specific tools like Docker, Kubernetes, or AWS.

For session-scoped capabilities, use `--with`:

```
$ aide --with k8s docker
```

For persistent capabilities, add them to a context in config:

```yaml
contexts:
  infra:
    agent: claude
    capabilities: [docker, k8s, aws]
```

See [Capabilities](capabilities.md) for details on built-in and custom capabilities.

## Creating Config Manually

`aide init` creates a minimal config without the full wizard:

```
$ aide init
Detected agents on PATH: claude
Primary agent (default: claude):
Set up secrets? (y/N): n

Created /home/user/.config/aide/config.yaml:
  agent: claude
```

The resulting file contains a single line:

```yaml
agent: claude
```

Add secrets, environment variables, and contexts by editing the file directly.

## Empty-State Experience

The first time you run `aide` in a folder that has no matching context configured,
aide detects the gap and guides you through fixing it rather than failing silently.

In a TTY you see:

```
aide: no context matches this folder.

What do you want to do?
  [1] Bind this folder to an existing context
  [2] Create a new context for this folder
  [3] Launch once with an existing context (don't save)
  [c] Cancel

Choose [1]:
```

Pick `[2]` to create a new context — aide walks you through naming it, picking
an agent (auto-detected if you have only one supported agent on PATH), optionally
binding a secret store, and attaching the current folder.

If you already have a context (say `work`) and want this folder to also resolve
to it, pick `[1]`, or run directly:

```
aide context bind work
```

By default `bind` matches by git remote URL when the folder is a git repo with
an `origin` remote — so the same context resolves correctly for any worktree
or fresh checkout of the same repo.

In non-interactive mode (CI, scripts) aide prints concrete next-command hints:

```
aide: no context matches this folder, and no default_context is configured.

To proceed, run one of:
  aide context bind <name>            # attach this folder to existing context
  aide context create [name]          # create a new context for this folder
  aide use <name> -- <agent-args>     # launch once without persisting
  aide context set-default <name>     # use a fallback for unmatched folders
```

## Creating a Context

`aide context create` creates a new context with an interactive wizard (TTY) or
fully scripted flags (non-TTY):

```
# Interactive wizard
$ aide context create

# Non-interactive — name, agent, and cwd binding all specified
$ aide context create work --agent claude --here

# Non-interactive — skip binding the current folder
$ aide context create work --agent claude --no-here
```

The first context created automatically becomes the `default_context`.

## Binding a Directory to an Existing Context

`aide context bind` attaches the current folder to a context that already exists:

```
$ cd ~/work/myproject
$ aide context bind work
Bound this folder to context "work" (matched by remote git@github.com:…/myproject.git)
```

Use `--path` to force an exact folder path match instead of a git remote match:

```
$ aide context bind work --path
Bound this folder to context "work" (matched by path /home/user/work/myproject)
```

## Binding a Directory (legacy aide use)

`aide use <agent>` creates or updates a context that matches the current directory.
For new setups, `aide context create` and `aide context bind` are preferred.

```
$ cd ~/work/myproject
$ aide use claude
Created context "myproject":
  agent: claude
  match: /home/user/work/myproject
```

Available flags:

```
--match "~/work/*"     Glob pattern instead of exact CWD
--secret work          Attach a named secrets file
--sandbox strict       Apply a sandbox profile
```

## Your First Multi-Context Setup

Two contexts: work uses AWS Bedrock, personal uses a direct API key.

```
# Work context: bind ~/work/* to claude with Bedrock credentials
$ aide secrets create work --age-key age1abc...
Created secrets/work.enc.yaml

$ aide use claude --match "~/work/*" --secret work
Created context "work":
  agent: claude
  match: ~/work/*
  secret: work

# Personal context: bind ~/oss/* to claude with Anthropic API key
$ aide secrets create personal --age-key age1abc...
Created secrets/personal.enc.yaml

$ aide use claude --match "~/oss/*" --secret personal
Created context "oss":
  agent: claude
  match: ~/oss/*
  secret: personal

# Set default for unmatched directories
$ aide context set-default oss
```

Edit `~/.config/aide/config.yaml` to add env var templates that pull values from secrets:

```yaml
contexts:
  work:
    agent: claude
    secret: work
    match:
      - path: ~/work/*
    env:
      CLAUDE_CODE_USE_BEDROCK: "1"
      AWS_PROFILE: "{{ .secrets.aws_profile }}"

  oss:
    agent: claude
    secret: personal
    match:
      - path: ~/oss/*
    env:
      ANTHROPIC_API_KEY: "{{ .secrets.anthropic_api_key }}"

default_context: oss
```

## Verifying

`aide which` shows which context matched and why:

```
$ cd ~/work/myproject
$ aide which
Context:  work
Matched:  path glob match: ~/work/*
Agent:    claude (/usr/local/bin/claude)
Secret:   work
Env:
  AWS_PROFILE              ← from secrets.aws_profile
  CLAUDE_CODE_USE_BEDROCK  ← literal
```

`aide validate` checks the config for errors:

```
$ aide validate
OK (2 contexts, 1 agents, 2 secrets)
```

Validation reports specific failures:

```
Errors:
  - context "work" references secret "work" which does not exist

1 errors, 0 warnings
```
