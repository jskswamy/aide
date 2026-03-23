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

Each context contains: `agent`, `secret`, `env`, `match`, and `sandbox`.

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

Env variables merge additively; the project override wins on key conflicts. All other fields replace the matched value when set.

## Managing Contexts

```
aide context list                         # list all contexts
aide context add                          # add a context interactively
aide context add-match                    # add a match rule (auto-detects CWD)
aide context add-match --context work     # target a specific context
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
