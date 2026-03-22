# Environment Variables

## Env Vars Live on Contexts

Agent definitions are binary-only. All env vars, secrets, and settings belong on
the context. The same `claude` binary uses different API keys in different contexts:

```yaml
agents:
  claude:
    binary: claude

contexts:
  personal:
    agent: claude
    env:
      ANTHROPIC_API_KEY: "{{ .secrets.personal_key }}"
  work:
    agent: claude
    env:
      ANTHROPIC_API_KEY: "{{ .secrets.work_key }}"
```

## Literal Values

Pass a plain string and aide forwards it unchanged. No secrets file required:

```yaml
env:
  CLAUDE_CODE_USE_BEDROCK: "1"
  AWS_REGION: "us-east-1"
```

## Template Syntax

Three variables are available inside `{{ }}` expressions:

- `{{ .secrets.key_name }}` resolves to the value of `key_name` from the encrypted secrets file.
- `{{ .project_root }}` resolves to the git repository root, or cwd if not in a repo.
- `{{ .runtime_dir }}` resolves to an ephemeral temp directory recreated on each launch.

Example using all three:

```yaml
contexts:
  work:
    agent: claude
    secret: work
    env:
      ANTHROPIC_API_KEY: "{{ .secrets.anthropic_key }}"
      PROJECT_ROOT: "{{ .project_root }}"
      AGENT_TMPDIR: "{{ .runtime_dir }}"
```

## Setting Env Vars

`aide env set KEY VALUE` writes a literal value to the CWD-matched context:

```sh
aide env set CLAUDE_CODE_USE_BEDROCK 1
aide env set AWS_REGION us-east-1
aide env set CONTEXT_VAR value --context work
```

## The --from-secret Flag

`--from-secret key_name` writes the template expression instead of a raw value.
The key must exist in the context's secrets file:

```sh
aide env set ANTHROPIC_API_KEY --from-secret anthropic_key
aide env set ANTHROPIC_API_KEY --from-secret anthropic_key --context work
```

Pass `--from-secret` without a value to open an interactive picker. aide decrypts
the secrets file and presents available keys:

```sh
aide env set ANTHROPIC_API_KEY --from-secret
# Available secret keys:
#   [1] anthropic_key
#   [2] openai_key
# Select secret key [1]:
```

If the context has no `secret` field set, aide prompts to select one before
showing the key picker.

## Listing Env Vars

`aide env list` shows all env vars for the CWD-matched context with source annotations:

```
Context: work
  ANTHROPIC_API_KEY   ← secrets.anthropic_key
  AWS_REGION          = us-east-1
  PROJECT_ROOT        ← project_root
  AGENT_TMPDIR        ← runtime_dir
```

Use `--context` to target a specific context:

```sh
aide env list --context work
```

## Removing Env Vars

`aide env remove KEY` deletes an env var from the CWD-matched context:

```sh
aide env remove ANTHROPIC_API_KEY
aide env remove AWS_REGION --context work
```

## Clean Env Mode

Enable with `--clean-env` at launch or `clean_env: true` in the sandbox config.
The agent starts with only aide-injected vars. Standard vars (`PATH`, `HOME`,
`SHELL`) are preserved. All other inherited shell environment is stripped:

```yaml
contexts:
  work:
    agent: claude
    sandbox:
      clean_env: true
```

Clean env mode prevents credential leakage from the parent shell and makes the
agent's environment fully reproducible across machines.
