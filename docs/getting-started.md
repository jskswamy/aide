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

## Binding a Directory

`aide use <agent>` creates or updates a context that matches the current directory:

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

The first context created by `aide use` automatically becomes the `default_context`.

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
