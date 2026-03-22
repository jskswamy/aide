# aide Configuration

## Config Location

aide reads its global configuration from `~/.config/aide/config.yaml`. If `$XDG_CONFIG_HOME` is set, aide uses `$XDG_CONFIG_HOME/aide/config.yaml` instead.

Encrypted secret files live under `~/.config/aide/secrets/`.

To override settings for a specific project, place a `.aide.yaml` file in the project root. aide walks up from the current directory and stops at the git root when searching for this file.

---

## Minimal Format

A flat file with no `agents:` or `contexts:` keys. aide treats it as a single default context.

```yaml
agent: claude
env:
  ANTHROPIC_API_KEY: "{{ .secrets.anthropic_api_key }}"
secret: personal
```

aide registers the agent binary automatically using the name provided. Credentials belong here, not on a separate agent definition.

---

## Full Format

Use the full format for multiple projects with different agents or credentials.

```yaml
agents:
  claude:
    binary: claude
  aider:
    binary: aider

contexts:
  work:
    match:
      - remote: "github.com/acme/*"
      - path: "~/work/*"
    agent: claude
    secret: work
    env:
      ANTHROPIC_API_KEY: "{{ .secrets.anthropic_api_key }}"
    sandbox:
      network: outbound
      writable:
        - "{{ .project_root }}"

  personal:
    match:
      - remote: "github.com/myuser/*"
    agent: claude
    secret: personal
    env:
      ANTHROPIC_API_KEY: "{{ .secrets.anthropic_api_key }}"
    sandbox: false

  open-source:
    match:
      - path: "~/oss/*"
    agent: aider
    secret: oss
    env:
      OPENAI_API_KEY: "{{ .secrets.openai_api_key }}"
    sandbox: strict

default_context: personal

preferences:
  show_info: true
  info_style: compact
  info_detail: normal
```

---

## Agent Definitions

The `agents:` block maps names to binaries. The agent name is the map key. `binary:` is the executable name or absolute path; if omitted, aide uses the agent name as the binary. Agents are binary definitions only. Credentials and environment variables belong on contexts.

Known agents that aide detects automatically on `PATH`: `claude`, `codex`, `aider`, `goose`, `amp`, `gemini`.

---

## Context Fields

- `match:`: list of path or remote rules that activate the context. Each rule sets one of `path:` (glob against CWD) or `remote:` (glob against git remote URL). `remote_name:` defaults to `origin`.
- `agent:`: agent name; must exist in `agents:`.
- `secret:`: secret file name resolved under `~/.config/aide/secrets/`.
- `env:`: environment variables passed to the agent; supports Go template syntax for secret injection.
- `sandbox:`: accepts `false` (disable), a string profile name (e.g. `strict`), or an inline policy mapping:

```yaml
sandbox:
  writable:
    - "{{ .project_root }}"
  network: outbound
  allow_subprocess: true
```

---

## Preferences

The top-level `preferences:` block controls the startup display.

- `show_info:` (bool, default `true`). Show the startup banner before launching the agent.
- `info_style:` (`compact` | `boxed` | `clean`, default `compact`). Banner style.
- `info_detail:` (`normal` | `detailed`, default `normal`). Banner verbosity.

Override any of these per-project in `.aide.yaml` under a `preferences:` key.

---

## Per-Project Override

`.aide.yaml` supports: `agent`, `env`, `secret`, `sandbox`, `preferences`. aide merges it on top of the matched global context.

- `env:` merges additively; project values win on key conflicts.
- All other fields replace the matched context value entirely.

---

## Validation

`aide validate` checks:

- Agent names in contexts exist in `agents:`.
- Secret files exist on disk.
- Sandbox profile names are defined in `sandboxes:`.
- Template syntax in `env:` values is valid.
- `env:` values referencing `.secrets.*` have a `secret:` configured on that context.

Warnings print separately from hard errors. The command exits non-zero on any error.

---

## Viewing and Editing

```
aide config show    # print the global config file
aide config edit    # open $EDITOR, validate after save
```
