# Provisioning

aide can declare which plugins and MCP servers a profile expects, then
reconcile that declaration against what is actually installed in the
agent. The model is the same Terraform-style "describe the desired
end state, then plan and apply": you write a config, run `aide sync
--plan` to see the diff, and `aide sync` to make it real.

## Why this exists

Plugin/MCP state is project-shaped, but agents store it machine-shaped: a
new laptop means reinstalling everything by hand and hoping you didn't
miss one, a new teammate gets a setup README instead of a source of
truth, and a client project sharing an agent binary with a personal
project means uninstalling and reinstalling plugins as you switch - or
polluting every profile with every plugin you've ever needed.

Declarative provisioning fixes this by treating plugins/MCP the way
Terraform treats infrastructure: declared in version-controlled config,
reconciled with a planned diff, recorded as state on disk. One command
brings any machine - yours, a teammate's, a fresh container - to the
exact state your config says it should be in.

## Why per-profile config dirs matter

Every coding agent reads from one fixed config dir by default
(`~/.claude`, `~/.gemini`, `~/.copilot`, etc.), holding plugins, MCP
servers, session history, and credentials for every project you run it
against. That means session history and MCP servers bleed across
projects, plugins with conflicting assumptions can't coexist per-project,
and every project shares the same model-provider credentials even when a
client requires separate billing.

Every major agent has the same workaround: an env var that swaps the
entire config tree - `CLAUDE_CONFIG_DIR` for Claude, `GEMINI_HOME` for
Gemini, `CODEX_HOME` for Codex, `COPILOT_HOME` for Copilot. Doing this by
hand means remembering each agent's env-var name, tilde-expanding paths
some agents don't expand themselves, and wiring the env var into your
shell init, your launch script, and the sandbox rules that grant access
to the custom dir - three places, three chances to drift.

aide's `config_dir_name: <name>` field (`profile:` under legacy
`contexts:`) collapses all of that. You write:

```yaml
profiles:
  work:
    agent: claude
    config_dir_name: work
```

and the driver does the rest: derives `~/.claude-work`, sets
`CLAUDE_CONFIG_DIR` at launch, propagates the env through
`aide sync` / `aide adopt` / `aide plugin list` so they all
target the right directory, and emits the seatbelt rule
that grants read/write access to the profile dir. Switch profile
and a different config dir takes over.

This is what makes the provisioning workflow above actually safe:
each profile's `aide sync` writes into the right config dir
without your having to think about which env var that agent
happens to use this week.

## What gets managed

Two top-level config blocks:

- **`plugins:`** - agent plugins or marketplaces (one shape per entry)
- **`mcp_servers:`** - MCP server definitions

Both live alongside `agents:` and `profiles:` in your
`~/.config/aide/config.yaml`. Profiles opt in to specific entries via
overrides.

The reconciler walks four state sources:

1. **Declared** - what your config says should exist
2. **Installed** - what the agent reports installed (via the driver's
   CLI surface)
3. **Managed** - what aide previously installed (recorded in
   `~/.local/state/aide/managed.json`)
4. **Adopted** - items installed by hand that you want aide to
   manage going forward

The diff between these produces the plan.

## Polymorphic plugin schema

The `plugins:` block reads each entry's value shape to decide what the
key means - no `type:` discriminator. Three shapes:

```yaml
plugins:
  # list value → marketplace + plugins to install from it
  steveyegge/beads: [beads]
  jskswamy/claude-plugins: [craft, devenv, jot, refactor]

  # string value → URL-direct install ref (Gemini-style agents)
  my-org/tool: "github:my-org/internal-tool"

  # null value → declare-only marketplace (ensure cached, install
  # nothing from it; useful when you want the marketplace registered
  # before adding plugins one at a time)
  obra/superpowers-marketplace: ~
```

For marketplace shape, the **key is a repo path** (`owner/repo`, a
full URL, or an absolute path to a local marketplace directory). aide
maps the key to the name the agent registers the marketplace under, so
`/abs/path/to/marketplace: [hello]` installs `hello@<marketplace name>`.
For URL-direct shape, the **key is a plugin name** the user
picks for readability.

## MCP servers schema

`mcp_servers:` always uses inline-table form (one shape):

```yaml
mcp_servers:
  postgres:
    command: postgres-mcp
    args: ["--port", "5432"]
  rfctl:
    command: rfctl
    args: [serve]
  github:
    url: "https://api.githubcopilot.com/mcp"
```

Each entry has either `command`+`args` (stdio MCP) or `url` (HTTP MCP).
`env:` per-server is also supported.

## Per-profile overrides

By default every profile sees the full top-level set. To customise
per-profile, use three delta keywords with deterministic composition:

```yaml
profiles:
  default:
    agent: claude
    # inherits everything declared at the top level

  work:
    agent: claude
    config_dir_name: work
    plugins:
      exclude:
        - obra/superpowers-marketplace/double-shot-latte
        - jskswamy/claude-plugins/refactor
      extra:
        my-org/internal: [client-tools]

  oss:
    agent: claude
    config_dir_name: oss
    plugins:
      only:
        - jskswamy/claude-plugins: [commit-tools, refactor]
        - steveyegge/beads: [beads]
```

Three keywords:

- **`exclude:`** - subtract from the inherited set. Path syntax
  `repo/plugin` reaches inside a marketplace entry, removing one
  plugin without touching the rest.
- **`extra:`** - add on top of inherited entries.
- **`only:`** - replace the inherited set entirely with this list.

The same three keywords apply to `mcp_servers:` blocks per-profile.
Entries use the full inline-table form (just like the top-level schema),
and `env:` values support `{{ .secrets.<name> }}` templating resolved
from the profile's `secret:` store at sync time.

`aide sync` only actually decrypts the secrets store and re-resolves
these templates when something relevant changed since the last
successful sync: either `config.yaml` or the encrypted secrets file
itself. When neither changed, sync skips decryption entirely and
substitutes the values already installed in the agent's config instead,
so routine no-op sync runs don't require the age key to be available.
One consequence: if an installed secret value is manually edited or
corrupted directly in the agent's own config, sync no longer heals it
automatically on the next run the way it used to. Run `aide sync
--force-secrets` to force re-resolution from the secrets store and
repair drift like that on demand.

```yaml
profiles:
  work:
    agent: claude
    config_dir_name: work
    secret: work
    mcp_servers:
      # add GitHub + Jira on top of the inherited set
      extra:
        github:
          command: npx
          args: ["-y", "@modelcontextprotocol/server-github"]
          env:
            GITHUB_PERSONAL_ACCESS_TOKEN: "{{ .secrets.github_token }}"
        jira:
          command: npx
          args: ["-y", "@modelcontextprotocol/server-jira"]
          env:
            JIRA_URL: "https://my-org.atlassian.net"
            JIRA_API_TOKEN: "{{ .secrets.jira_token }}"

  oss:
    agent: claude
    config_dir_name: oss
    mcp_servers:
      # OSS work only needs GitHub - replace the inherited set
      only: [github]
```

Keep the literal token inline (`GITHUB_PERSONAL_ACCESS_TOKEN: "ghp_…"`)
only for throwaway tests; prefer the `{{ .secrets.<name> }}` form so the
value stays out of version-controlled config.

## CLI commands

Four commands drive the workflow.

### `aide sync`

Plan-then-apply reconciliation.

```
aide sync                  # interactive: shows plan, asks for confirmation
aide sync --plan           # plan-only; never mutates state
aide sync --yes            # non-interactive (CI / scripts)
aide sync --profile oss    # operate on a specific profile (default: matched-by-CWD)
```

Marketplace adds are sequenced before plugin installs. Each successful
operation records an inverse in an in-memory journal; on any failure
the engine walks the journal in reverse and rolls back, then prints
the failing op and a retry hint. State is persisted to
`managed.json` atomically and only on full success.

Sample plan output:

```
Plan for profile work (agent: claude):

  + install marketplace jskswamy/claude-plugins
  + install plugin commit-tools (from jskswamy/claude-plugins)
  + install plugin craft         (from jskswamy/claude-plugins)
  - uninstall plugin refactor    (no longer declared)

  unmanaged plugin gopls-lsp     (installed in agent, not declared,
                                  not previously managed - run `aide
                                  adopt` to bring under management)
```

### `aide adopt`

Promote agent-installed but undeclared items into `config.yaml`.

```
aide adopt                 # interactive: prompts per item
aide adopt --yes           # accept everything
aide adopt --profile work
```

For marketplace-class agents, adopted plugins land under the right
repo key in list-valued form (looked up via the driver's
`InstalledMarketplaces`). For URL-direct agents (Gemini), adopted
plugins get string-valued entries.

`aide adopt` is the bridge between manual setup and managed state -
if you previously installed plugins by hand, run adopt once and
subsequent `aide sync` runs treat them as known.

### `aide plugin list`

Three-column view (declared / installed / managed) per profile.

```
aide plugin list                # current profile
aide plugin list --profile work
```

For marketplace agents the output includes a MARKETPLACES section
first, surfacing the agent's canonical marketplace name (e.g.
`beads-marketplace` for `steveyegge/beads`) and flagging installed-
but-undeclared marketplaces as `unmanaged`.

### `aide mcp list`

Same shape as `aide plugin list` but for MCP servers.

```
aide mcp list
aide mcp list --profile work
```

## State file

aide records what it manages in `managed.json` in its state directory
(`$XDG_STATE_HOME/aide`, default `~/.local/state/aide`).
Schema (abbreviated):

```json
{
  "version": 2,
  "profiles": {
    "default": {
      "config_hash": "sha256:...",
      "synced_at": "2026-05-18T...",
      "plugins":      {...},
      "mcp_servers":  {...},
      "marketplaces": {...}
    },
    "work":    { ... },
    "oss":     { ... }
  }
}
```

Version 1 files keyed the same entries under `contexts`; aide still reads
them and writes version 2 on the next sync. `aide profile migrate --write`
updates `config_hash` for profiles synced against the old `config.yaml`,
so converting the file is not reported as drift. A file from a newer
aide stops with `unsupported provisioning state version` and is left
alone.

`config_hash` and `synced_at` are **per-profile** - a successful
sync of one profile never silences drift signals for another. The
file is written atomically (rename-into-place) and only after a
sync completes end-to-end.

## Drift detection

`aide which` shows a one-line banner under the active profile when
the profile is out of sync. Two cheap signals fire it:

1. The profile's recorded `config_hash` differs from the current
   `config.yaml` hash (someone edited the config).
2. The desired set computed for the profile has items not yet
   recorded as managed in `managed.json` (sync never ran, or new
   declarations were added).

Both checks are in-process - no agent CLI poll, no network call. The
banner just says "config changed since last sync - run `aide sync`"
or "never synced - run `aide sync` to install declared plugins/MCP
servers".

## Per-agent capability matrix

Not every agent supports every shape. The provisioner driver advertises
what it consumes via `SupportedSourceShapes`.

| Agent | Plugin marketplaces | URL-direct plugins | MCP servers |
|---|---|---|---|
| `claude` | ✅ | - | ✅ (`~/.claude.json`) |
| `copilot` | ✅ | - | ✅ (`~/.copilot/mcp-config.json`) |
| `codex` | ✅ (via TOML edit) | - | ✅ (`~/.codex/config.toml`) |
| `gemini` | - | ✅ (`extensions` block) | ✅ (`~/.gemini/settings.json`) |
| `cursor-agent` | - (no plugin surface) | - | partial (see below) |

For `cursor-agent`, MCP lives at `~/.cursor/mcp.json` globally and
`.cursor/mcp.json` per-project. The provisioner driver for cursor is
tracked separately (the agent has no plugin/marketplace CLI surface;
project-scope MCP handling lands with the project-override work).

If a profile declares a shape the driver doesn't support (e.g.
URL-direct plugins for `claude`), `aide sync --plan` reports a
capability mismatch and aborts before any installation.

## Profile interaction

When a profile declares `config_dir_name: <name>`, the driver injects its
config-dir env var (`CLAUDE_CONFIG_DIR=~/.claude-<name>` for claude,
`GEMINI_HOME=~/.gemini-<name>` for gemini, etc.) into the resolved
profile env. The agent CLI shelled out by `aide sync` then writes to
that profile-specific config dir.

This is why profiles with their own config dirs work end-to-end: `aide sync
--profile work` writes Claude plugins to `~/.claude-work/` while
`aide sync --profile oss` writes to `~/.claude-oss/`, even though
both profiles use `agent: claude`.

See [Profiles](profiles.md) for the `config_dir_name` field details.

## Trust boundary

Provisioning operates on the global `~/.config/aide/config.yaml`. The
project-scope `.aide.yaml` does **not** participate in sync today -
its plugin/MCP declarations are ignored by `aide sync`. This is a
deliberate trust boundary: a malicious `.aide.yaml` in a repo you
just cloned cannot cause aide to install agent plugins or MCP servers
without your action. Merging trusted `.aide.yaml` declarations into
sync is tracked separately.

## Out of scope (today)

- Goose, Amp, and Aider provisioning - drivers not implemented yet.
- Project-scope `.aide.yaml` plugin/MCP merging into `aide sync` -
  separate work (trust gate).
- Plugin pinning by version - sync currently installs whatever the
  agent's marketplace returns for a given plugin name.
- Cross-agent migration (Claude plugin → Gemini extension) - out of
  scope; agents have different runtime models.

## Examples

### Minimal: one profile, three plugins

```yaml
agents:
  claude:
    binary: claude

plugins:
  jskswamy/claude-plugins: [commit-tools, craft, refactor]

profiles:
  default:
    agent: claude
```

Run `aide sync` once and the three plugins land in `~/.claude/`.

### Multi-profile: same agent, different config dirs

```yaml
plugins:
  jskswamy/claude-plugins: [commit-tools, craft, jot, refactor]
  steveyegge/beads: [beads]

profiles:
  work:
    agent: claude
    config_dir_name: work
    # → CLAUDE_CONFIG_DIR=~/.claude-work
    # `aide sync --profile work` installs the four plugins there

  oss:
    agent: claude
    config_dir_name: oss
    plugins:
      exclude:
        - jskswamy/claude-plugins/refactor  # no refactor on OSS
    # → CLAUDE_CONFIG_DIR=~/.claude-oss
    # `aide sync --profile oss` installs three plugins there
```

### Adoption: bring an existing setup under management

```bash
# You already installed claude plugins by hand. Run once:
aide adopt --yes

# config.yaml now lists everything that was installed.
# Subsequent syncs treat them as managed:
aide sync --plan
```
