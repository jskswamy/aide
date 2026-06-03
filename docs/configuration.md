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
    icon: "🤖"
  aider:
    binary: aider

contexts:
  work:
    match:
      - remote: "github.com/acme/*"
      - path: "~/work/*"
    icon: "💼"
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
    icon: "🏠"
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

```yaml
agents:
  claude:
    binary: claude
    icon: "🤖"        # optional; shown in banner and prompt
  aider:
    binary: aider
```

The `icon` field (optional) sets a Unicode symbol displayed alongside the agent name in the banner and `aide prompt` output.

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
- `hooks:` (map) — Declarative hook set. Keys are normalized event names (`pre_tool`, `post_tool`, `session_start`, `session_end`, `notification`, `stop`); values are lists of hook entries:

```yaml
hooks:
  pre_tool:
    - command: "rtk hook {agent}"
      matcher: shell   # optional: "shell" → agent-native name (e.g. Bash)
      timeout: 30      # optional: seconds; 0 = driver default
```

The `{agent}` template variable is replaced with the resolved agent name for each context. Reconciled by `aide sync`. See [Hooks](#hooks) below.

- `plugins:` (map) — Declarative plugin set per agent. Value shape per entry decides the meaning: list = marketplace + plugin names, string = URL-direct install ref, null = declare-only marketplace. Reconciled by `aide sync`. See [Provisioning](provisioning.md).
- `mcp_servers:` (map) — Declarative MCP server set. Each entry is an inline table with `command`+`args` (stdio) or `url` (HTTP), plus optional `env`. Reconciled by `aide sync`.
- `sandboxes:` (map) — Named sandbox profiles referenced from contexts (`sandbox: <name>`).
- `custom_guards:` and `guard_types:` (advanced) — Define custom seatbelt guard modules. Most users should not need these.

---

## Context Fields

- `match:`: list of path or remote rules that activate the context. Each rule sets one of `path:` (glob against CWD) or `remote:` (glob against git remote URL). `remote_name:` defaults to `origin`.
- `agent:`: agent name; must exist in `agents:`.
- `secret:`: secret file name resolved under `~/.config/aide/secrets/`.
- `env:`: environment variables passed to the agent; supports Go template syntax for secret injection.
- `icon:` (string, optional): Unicode symbol displayed alongside the context name in the banner and `aide prompt` output.
- `profile:` (string, optional): per-context agent profile name. Driver derives the agent's config-dir env var (e.g. `CLAUDE_CONFIG_DIR=~/.claude-<name>`) and injects it at launch + sync + adopt + list. Avoids hand-rolling per-agent env vars. See [Provisioning § Profile interaction](provisioning.md#profile-interaction) and [Contexts](contexts.md).
- `profile_dir:` (string, optional): override the derived `~/.<agent>-<name>` path with an explicit absolute or HOME-rooted path. Requires `profile:` to also be set.
- `plugins:` / `mcp_servers:` (per-context overrides, optional): mapping with `extra:` / `exclude:` / `only:` deltas applied on top of the top-level set. See [Provisioning](provisioning.md).
- `hooks:` (per-context override, optional): delta applied on top of the top-level `hooks:` map.

```yaml
contexts:
  work:
    hooks:
      exclude: [session_start]   # drop a top-level event entirely
      extra:
        pre_tool:
          - command: "notify work-hook {agent}"
```
- `capabilities:` (list) — Capability names to activate for this context (e.g. `[docker, k8s]`). See [Capabilities](capabilities.md) for details.
- `yolo:` (bool, optional): skip agent permission checks for this context. The agent-specific flag is injected automatically (e.g. `--dangerously-skip-permissions` for Claude). The OS sandbox remains active.
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

## Hooks

Hooks inject commands into agent lifecycle events. aide writes them to the
agent's config file during `aide sync`; the agent runs them, not aide.

### Supported events

| Event | Claude | Gemini | Cursor | Copilot | Hermes |
|-------|--------|--------|--------|---------|--------|
| `pre_tool` | `PreToolUse` | `BeforeTool` (script) | `preToolUse` | `PreToolUse` (file) | `pre_tool_call` (plugin) |
| `post_tool` | `PostToolUse` | — | — | — | — |
| `session_start` | `SessionStart` | — | — | — | — |
| `session_end` | `SessionEnd` | — | — | — | — |
| `notification` | `Notification` | — | — | — | — |
| `stop` | `Stop` | — | — | — | — |

Events not supported by an agent are silently skipped during sync.

### Supported matchers

| Matcher | Claude | Cursor |
|---------|--------|--------|
| `shell` | `Bash` | `Shell` |

Matchers not supported by an agent are ignored.

### Storage formats per agent

- **Claude** — merged into `~/.claude/settings.json` (or `$CLAUDE_CONFIG_DIR/settings.json`) under the `hooks` key. Ownership is tracked in `managed.json`; user-added entries are never touched.
- **Gemini** — individual `aide_<hash>.sh` scripts written to `~/.gemini/hooks/`. Only `pre_tool` is supported.
- **Cursor** — merged into `~/.cursor/hooks.json`. Ownership is tracked in `managed.json`; user-added entries are never touched.
- **Copilot** — individual `aide-<hash>.json` files written to `~/.config/copilot/hooks/`. Only `pre_tool` is supported.
- **Hermes** — Python plugin directories written to `~/.hermes/plugins/aide_<hash>/` (an `__init__.py` + `plugin.yaml` per hook). Only `pre_tool` is supported.
- **Codex** — no hook support.

### The `{agent}` template variable

The command field supports `{agent}`, replaced with the context's resolved
agent name at sync time:

```yaml
hooks:
  pre_tool:
    - command: "rtk hook {agent}"
```

This lets a single top-level hook declaration work across all your contexts
without per-context duplication.

### Management

```bash
aide hook list                                          # show declared hooks
aide hook add --event pre_tool --command "rtk hook {agent}"  # add a hook
aide hook remove --event pre_tool --command "rtk hook {agent}"  # remove a hook
aide sync                                               # write to agent config
```

---

## Viewing and Editing

```
aide config show    # print the global config file
aide config edit    # open $EDITOR, validate after save
```
