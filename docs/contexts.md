# Contexts

## What is a Context

A context binds an agent, credentials, and sandbox policy to a set of directories or git remotes. When you run `aide`, it resolves which context applies to your current working directory and launches the correct agent with the right environment.

Contexts live in `~/.config/aide/config.yaml` under the `contexts:` key:

```yaml
agents:
  claude:
    binary: claude

contexts:
  work:
    agent: claude
    secret: work
    match:
      - path: ~/work/*
      - remote: "github.com/acme/*"
    env:
      ANTHROPIC_API_KEY: "{{ .secrets.ANTHROPIC_API_KEY }}"

default_context: work
```

Each context contains: `agent`, `secret`, `env`, `match`, `sandbox`, `capabilities`, and optionally `yolo`.

## Match Rules

Two rule types select a context based on the current working directory or git remote.

**Path rules** match the working directory. Tilde expands to the home directory.

```yaml
match:
  - path: ~/work/*        # any subdirectory of ~/work
  - path: ~/work/acme     # exact directory
  - path: ~/clients/**    # recursive glob
```

**Remote rules** match the git remote URL. Override the default remote name with `remote_name`.

```yaml
match:
  - remote: "github.com/acme/*"
  - remote: "github.com/acme/api"      # exact match
  - remote: "*.internal.corp/*"
    remote_name: upstream
```

## Resolution Order

aide scores every match rule and picks the highest-scoring context:

| Match type | Base score | Tiebreaker |
|------------|-----------|------------|
| Exact path | 300 | + pattern length |
| Glob path  | 200 | + pattern length |
| Remote     | 100 | + pattern length |

Longer patterns beat shorter patterns within the same tier. If no rule matches, `default_context` is the fallback. A `.aide.yaml` project file wins over everything.

## Per-Project Override

Place `.aide.yaml` at the project root to override fields in the resolved context for that project. aide merges it on top of the matched context.

```yaml
# .aide.yaml
agent: claude
secret: client-x
env:
  NODE_ENV: development
sandbox:
  writable_extra:
    - /tmp/build
```

Env variables merge additively; the project override wins on key conflicts. All other fields (including `yolo`) replace the matched value when set.

## Capabilities

The `capabilities` field activates named capabilities for a context. Capabilities grant the sandbox additional permissions (filesystem paths, environment variables, network access) for specific tools.

```yaml
contexts:
  infra:
    agent: claude
    match:
      - path: ~/infra/*
    capabilities: [docker, k8s, aws]
    secret: work
    env:
      ANTHROPIC_API_KEY: "{{ .secrets.anthropic_api_key }}"
```

See [Capabilities](capabilities.md) for details on built-in and custom capabilities.

## Managing Contexts

```
aide context list                         # list all contexts
aide context create                       # create a new context interactively
aide context create work --agent claude --here  # scripted: create and bind CWD
aide context bind work                    # attach this folder to an existing context
aide context bind work --path             # force exact folder path match
aide context set-secret personal          # set secret (auto-detects CWD)
aide context set-secret work --context work
aide context remove-secret                # clear secret (auto-detects CWD)
aide context set-default work             # change the default fallback
aide context rename work work-acme        # rename (updates default_context too)
aide context remove personal              # delete a context
```

`aide context list` marks the default context:

```
personal
  Agent:  claude
  Match:  ~/personal/*

work (default)
  Agent:  claude
  Secret: work
  Match:  ~/work/*
```

## The `aide use` Shortcut

`aide use` creates or updates a context named after the CWD basename and binds it in one step. The first context created automatically becomes `default_context`.

```
aide use claude                       # bind CWD to claude
aide use claude --match "~/work/*"    # bind a glob pattern instead of CWD
aide use claude --secret work         # also attach a secret
aide use claude --sandbox strict      # use a named sandbox profile
aide use --context work               # add CWD as a match rule to existing context
```

## Multi-Account Setups

A common need is keeping a personal and a work Claude account side by
side — different API keys, different MCP servers, separate
conversation history, no cross-contamination. aide composes two
primitives to make this clean:

1. **One secret store per account.** Each holds its own
   `anthropic_api_key`. Use a different age recipient if you want
   tighter access boundaries; the same recipient is fine if only the
   API keys differ.
2. **One context per account**, each setting `CLAUDE_CONFIG_DIR` to a
   distinct path so Claude Code's own state (auth tokens, settings,
   MCP servers, conversation history) stays isolated. `CLAUDE_CONFIG_DIR`
   is a Claude Code env var; aide just sets it per context.

**Walkthrough — personal + work.**

Create one encrypted secret per account (see [Secrets — Quick
Start](secrets.md#quick-start-wiring-an-anthropic-api-key) for the
editor flow):

```sh
aide secrets create personal --age-key age1abc...
aide secrets create work     --age-key age1xyz...
```

Bind directories and wire env per context:

```sh
# Personal — everything under ~/personal/*
aide use claude --match "~/personal/*" --secret personal
aide env set ANTHROPIC_API_KEY --secret-key anthropic_api_key --context personal --global
aide env set CLAUDE_CONFIG_DIR "$HOME/.claude-personal"       --context personal --global

# Work — everything under ~/work/*
aide use claude --match "~/work/*" --secret work
aide env set ANTHROPIC_API_KEY --secret-key anthropic_api_key --context work --global
aide env set CLAUDE_CONFIG_DIR "$HOME/.claude-work"           --context work --global
```

The first launch in each tree authenticates Claude Code into the
matching `CLAUDE_CONFIG_DIR`; from then on aide picks the right state
directory automatically based on CWD.

```sh
cd ~/personal/some-project && aide   # uses ~/.claude-personal
cd ~/work/some-project    && aide    # uses ~/.claude-work
```

Verify with `aide which --resolve`:

```
$ cd ~/work/some-project && aide which --resolve
Context:  work
Matched:  path glob match: ~/work/*
Env:
  ANTHROPIC_API_KEY  <- secret (sk-ant-........)
  CLAUDE_CONFIG_DIR  /Users/you/.claude-work
```

Resulting layout:

```
~/.config/aide/secrets/personal.enc.yaml
~/.config/aide/secrets/work.enc.yaml
~/.claude-personal/   ← Claude Code state for personal account
~/.claude-work/       ← Claude Code state for work account
```

The same pattern extends to N accounts (client-x, oss, ...) — just
add more context blocks. Codex and other agents have analogous config
directory env vars (`CODEX_HOME`, etc.); set those the same way.

## Default Context

The default context is the fallback when no match rule applies. Change it at any time:

```
aide context set-default personal
```

`aide context list` shows it with a `(default)` marker.

## Inspecting

`aide which` shows the matched context and the reason for the match:

```
$ aide which
Context:  work
Matched:  path glob match: ~/work/*
Agent:    claude
Secret:   work
```

`aide which --resolve` decrypts the secret and shows resolved environment variable values (redacted):

```
$ aide which --resolve
Context:  work
Matched:  path glob match: ~/work/*
Agent:    claude (/usr/local/bin/claude)
Secret:   work  [keys: ANTHROPIC_API_KEY]
Env:
  ANTHROPIC_API_KEY  <- secret (sk-ant-........)
```
