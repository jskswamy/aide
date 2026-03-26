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
yolo: true          # optional: skip agent permission checks
auto_approve: true  # alias for yolo
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

  infra:
    match:
      - path: "~/infra/*"
    agent: claude
    secret: work
    capabilities: [docker, k8s]
    auto_approve: true

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

## Top-Level Config Fields

In addition to `agents:`, `contexts:`, `default_context:`, and `preferences:`, the following top-level fields are available:

- `capabilities:` (map) — User-defined capability definitions. Each key is a capability name mapping to a `CapabilityDef`. Example:

```yaml
capabilities:
  k8s-dev:
    extends: k8s
    readable: ["~/.kube/dev-config"]
    deny: ["~/.kube/prod-config"]
    env_allow: [KUBECONFIG]
```

- `never_allow:` (list) — Paths that no capability can ever access. These are enforced globally regardless of which capabilities are active.
- `never_allow_env:` (list) — Environment variables always stripped from the agent process, even if a capability would otherwise permit them.

---

## Context Fields

- `match:`: list of path or remote rules that activate the context. Each rule sets one of `path:` (glob against CWD) or `remote:` (glob against git remote URL). `remote_name:` defaults to `origin`.
- `agent:`: agent name; must exist in `agents:`.
- `secret:`: secret file name resolved under `~/.config/aide/secrets/`.
- `env:`: environment variables passed to the agent; supports Go template syntax for secret injection.
- `capabilities:` (list) — Capability names to activate for this context (e.g. `[docker, k8s]`). See [Capabilities](capabilities.md) for details.
- `yolo:` (bool, optional): skip agent permission checks for this context. The agent-specific flag is injected automatically (e.g. `--dangerously-skip-permissions` for Claude). The OS sandbox remains active.
- `auto_approve:` (bool, optional): alias for `yolo`. Run agent without permission checks.
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
- `yolo:` (bool, optional). Global default for yolo mode. Context-level and project-level `yolo:` override this.

Override any of these per-project in `.aide.yaml` under a `preferences:` key.

---

## Per-Project Override

`.aide.yaml` supports: `agent`, `env`, `secret`, `sandbox`, `preferences`, `yolo`, `capabilities`, `disabled_capabilities`. aide merges it on top of the matched global context.

- `env:` merges additively; project values win on key conflicts.
- All other fields replace the matched context value entirely.

### Trust gate

`.aide.yaml` files are not applied until trusted. On first encounter, aide shows the file's contents and skips it. Run `aide trust` to approve the file, or `aide deny` to block it permanently. See [Sandbox](sandbox.md#trust-gate) for details.

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
