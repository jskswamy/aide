# Provisioning

aide can declare which plugins and MCP servers a context expects, then
reconcile that declaration against what is actually installed in the
agent. The model is the same Terraform-style "describe the desired
end state, then plan and apply": you write a config, run `aide sync
--plan` to see the diff, and `aide sync` to make it real.

## Why this exists

Agent plugins and MCP servers are project-shaped concerns that today
live as machine-shaped state. The split breaks down in three ways:

**Across machines.** Set up a new laptop and you go through
`claude plugin install <foo>` ten times, then realise two weeks
later that you forgot one and that's why your commit messages look
different here. Then the question becomes: which machine is
"correct"? There's no source of truth.

**Across teammates.** Onboarding a new engineer means writing a
setup README that lists the plugins to install and the MCP servers
to configure. Six months in, half the team installed the plugins,
the other half forgot, and a third group installed a different
version. Diagnosing "why does this prompt produce different output
on your machine vs. mine" becomes archaeology.

**Across contexts.** Working on a client project with its own
sandbox of plugins (a private MCP server, an internal-tools
marketplace) and a personal side project with a completely
different set, all running on the same agent binary — today there's
no per-project boundary. You're either uninstalling and reinstalling
plugins as you switch projects, or polluting every context with
every plugin you've ever needed.

Declarative provisioning solves all three by treating plugins/MCP
the same way Terraform treats infrastructure: declared in version-
controlled config, reconciled with a planned diff, recorded as
state on disk. One command brings any machine — yours, a teammate's,
a fresh container — to the exact state your config says it should be
in.

## Why per-context profiles matter

The other half of the workflow is profile isolation.

Every coding agent reads from a fixed config dir by default:
`~/.claude`, `~/.gemini`, `~/.copilot`, etc. That single dir holds
plugins, MCP servers, session history, and credentials for every
project you run the agent against. The result:

- **Cross-project bleed.** Your work session history shows up
  during your personal-project session. The MCP server you
  configured for one client is visible to all the others.
- **Hostile plugin coexistence.** A plugin you need for client A
  has a strong opinion that conflicts with what client B's prompt
  conventions assume. Today you uninstall one to use the other.
- **Credential leakage risk.** All projects share the same API
  keys for the underlying model provider. If a client mandates a
  separate billing account, you have no clean way to honour it.

Every major coding agent has the same workaround: a single env
var that swaps the entire config tree. Claude reads
`CLAUDE_CONFIG_DIR`; Gemini reads `GEMINI_HOME`; Codex reads
`CODEX_HOME`; Copilot reads `COPILOT_HOME`. Point it at
`~/.claude-work` instead of `~/.claude` and you get a fully
isolated profile.

Doing this manually is fragile:

- You have to remember each agent's env-var name (they don't
  match each other).
- You have to remember to tilde-expand because some agents don't
  do it themselves (this caused real bugs).
- You have to wire the env into your shell init, then again into
  whichever script launches the agent, then again into the
  sandbox rules so the sandbox actually allows reads from the
  custom dir.

aide's `profile: <name>` field collapses all of that. You write:

```yaml
contexts:
  work:
    agent: claude
    profile: work
```

and the driver does the rest: derives `~/.claude-work`, sets
`CLAUDE_CONFIG_DIR` at launch, propagates the env through
`aide sync` / `aide adopt` / `aide plugin list` so they all
target the right directory, and emits the seatbelt rule
that grants read/write access to the profile dir. Switch context
and a different profile takes over.

This is what makes the provisioning workflow above actually safe:
each context's `aide sync` writes into the right config dir
without your having to think about which env var that agent
happens to use this week.

## What gets managed

Two top-level config blocks:

- **`plugins:`** — agent plugins or marketplaces (one shape per entry)
- **`mcp_servers:`** — MCP server definitions

Both live alongside `agents:` and `contexts:` in your
`~/.config/aide/config.yaml`. Contexts opt in to specific entries via
overrides.

The reconciler walks four state sources:

1. **Declared** — what your config says should exist
2. **Installed** — what the agent reports installed (via the driver's
   CLI surface)
3. **Managed** — what aide previously installed (recorded in
   `~/.local/state/aide/managed.json`)
4. **Adopted** — items installed by hand that you want aide to
   manage going forward

The diff between these produces the plan.

## Polymorphic plugin schema

The `plugins:` block reads each entry's value shape to decide what the
key means — no `type:` discriminator. Three shapes:

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

For marketplace shape, the **key is a repo path** (`owner/repo` or a
full URL). For URL-direct shape, the **key is a plugin name** the user
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

## Per-context overrides

By default every context sees the full top-level set. To customise
per-context, use three delta keywords with deterministic composition:

```yaml
contexts:
  default:
    agent: claude
    # inherits everything declared at the top level

  work:
    agent: claude
    profile: work
    plugins:
      exclude:
        - obra/superpowers-marketplace/double-shot-latte
        - jskswamy/claude-plugins/refactor
      extra:
        my-org/internal: [client-tools]

  oss:
    agent: claude
    profile: oss
    plugins:
      only:
        - jskswamy/claude-plugins: [commit-tools, refactor]
        - steveyegge/beads: [beads]
```

Three keywords:

- **`exclude:`** — subtract from the inherited set. Path syntax
  `repo/plugin` reaches inside a marketplace entry, removing one
  plugin without touching the rest.
- **`extra:`** — add on top of inherited entries.
- **`only:`** — replace the inherited set entirely with this list.

The same three keywords apply to `mcp_servers:` blocks per-context.
Entries use the full inline-table form (just like the top-level schema),
and `env:` values support `{{ .secrets.<name> }}` templating resolved
from the context's `secret:` store at sync time:

```yaml
contexts:
  work:
    agent: claude
    profile: work
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
    profile: oss
    mcp_servers:
      # OSS work only needs GitHub — replace the inherited set
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
aide sync --context oss    # operate on a specific context (default: matched-by-CWD)
```

Marketplace adds are sequenced before plugin installs. Each successful
operation records an inverse in an in-memory journal; on any failure
the engine walks the journal in reverse and rolls back, then prints
the failing op and a retry hint. State is persisted to
`managed.json` atomically and only on full success.

Sample plan output:

```
Plan for context work (agent: claude):

  + install marketplace jskswamy/claude-plugins
  + install plugin commit-tools (from jskswamy/claude-plugins)
  + install plugin craft         (from jskswamy/claude-plugins)
  - uninstall plugin refactor    (no longer declared)

  unmanaged plugin gopls-lsp     (installed in agent, not declared,
                                  not previously managed — run `aide
                                  adopt` to bring under management)
```

### `aide adopt`

Promote agent-installed but undeclared items into `config.yaml`.

```
aide adopt                 # interactive: prompts per item
aide adopt --yes           # accept everything
aide adopt --context work
```

For marketplace-class agents, adopted plugins land under the right
repo key in list-valued form (looked up via the driver's
`InstalledMarketplaces`). For URL-direct agents (Gemini), adopted
plugins get string-valued entries.

`aide adopt` is the bridge between manual setup and managed state —
if you previously installed plugins by hand, run adopt once and
subsequent `aide sync` runs treat them as known.

### `aide plugin list`

Three-column view (declared / installed / managed) per context.

```
aide plugin list                # current context
aide plugin list --context work
```

For marketplace agents the output includes a MARKETPLACES section
first, surfacing the agent's canonical marketplace name (e.g.
`beads-marketplace` for `steveyegge/beads`) and flagging installed-
but-undeclared marketplaces as `unmanaged`.

### `aide mcp list`

Same shape as `aide plugin list` but for MCP servers.

```
aide mcp list
aide mcp list --context work
```

## State file

aide records what it manages in `~/.local/state/aide/managed.json`.
Schema (abbreviated):

```json
{
  "version": 1,
  "contexts": {
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

`config_hash` and `synced_at` are **per-context** — a successful
sync of one context never silences drift signals for another. The
file is written atomically (rename-into-place) and only after a
sync completes end-to-end.

## Drift detection

`aide which` shows a one-line banner under the active context when
the context is out of sync. Two cheap signals fire it:

1. The context's recorded `config_hash` differs from the current
   `config.yaml` hash (someone edited the config).
2. The desired set computed for the context has items not yet
   recorded as managed in `managed.json` (sync never ran, or new
   declarations were added).

Both checks are in-process — no agent CLI poll, no network call. The
banner just says "config changed since last sync — run `aide sync`"
or "never synced — run `aide sync` to install declared plugins/MCP
servers".

## Per-agent capability matrix

Not every agent supports every shape. The provisioner driver advertises
what it consumes via `SupportedSourceShapes`.

| Agent | Plugin marketplaces | URL-direct plugins | MCP servers |
|---|---|---|---|
| `claude` | ✅ | — | ✅ (`~/.claude.json`) |
| `copilot` | ✅ | — | ✅ (`~/.copilot/mcp-config.json`) |
| `codex` | ✅ (via TOML edit) | — | ✅ (`~/.codex/config.toml`) |
| `gemini` | — | ✅ (`extensions` block) | ✅ (`~/.gemini/settings.json`) |
| `cursor-agent` | — (no plugin surface) | — | partial (see below) |

For `cursor-agent`, MCP lives at `~/.cursor/mcp.json` globally and
`.cursor/mcp.json` per-project. The provisioner driver for cursor is
tracked separately (the agent has no plugin/marketplace CLI surface;
project-scope MCP handling lands with the project-override work).

If a context declares a shape the driver doesn't support (e.g.
URL-direct plugins for `claude`), `aide sync --plan` reports a
capability mismatch and aborts before any installation.

## Profile interaction

When a context declares `profile: <name>`, the driver injects its
config-dir env var (`CLAUDE_CONFIG_DIR=~/.claude-<name>` for claude,
`GEMINI_HOME=~/.gemini-<name>` for gemini, etc.) into the resolved
context env. The agent CLI shelled out by `aide sync` then writes to
that profile-specific config dir.

This is why multi-profile contexts work end-to-end: `aide sync
--context work` writes Claude plugins to `~/.claude-work/` while
`aide sync --context oss` writes to `~/.claude-oss/`, even though
both contexts use `agent: claude`.

See [Contexts](contexts.md) for the `profile` field details.

## Trust boundary

Provisioning operates on the global `~/.config/aide/config.yaml`. The
project-scope `.aide.yaml` does **not** participate in sync today —
its plugin/MCP declarations are ignored by `aide sync`. This is a
deliberate trust boundary: a malicious `.aide.yaml` in a repo you
just cloned cannot cause aide to install agent plugins or MCP servers
without your action. Merging trusted `.aide.yaml` declarations into
sync is tracked separately.

## Out of scope (today)

- Goose, Amp, and Aider provisioning — drivers not implemented yet.
- Project-scope `.aide.yaml` plugin/MCP merging into `aide sync` —
  separate work (trust gate).
- Plugin pinning by version — sync currently installs whatever the
  agent's marketplace returns for a given plugin name.
- Cross-agent migration (Claude plugin → Gemini extension) — out of
  scope; agents have different runtime models.

## Examples

### Minimal: one context, three plugins

```yaml
agents:
  claude:
    binary: claude

plugins:
  jskswamy/claude-plugins: [commit-tools, craft, refactor]

contexts:
  default:
    agent: claude
```

Run `aide sync` once and the three plugins land in `~/.claude/`.

### Multi-profile: same agent, different config dirs

```yaml
plugins:
  jskswamy/claude-plugins: [commit-tools, craft, jot, refactor]
  steveyegge/beads: [beads]

contexts:
  work:
    agent: claude
    profile: work
    # → CLAUDE_CONFIG_DIR=~/.claude-work
    # `aide sync --context work` installs the four plugins there

  oss:
    agent: claude
    profile: oss
    plugins:
      exclude:
        - jskswamy/claude-plugins/refactor  # no refactor on OSS
    # → CLAUDE_CONFIG_DIR=~/.claude-oss
    # `aide sync --context oss` installs three plugins there
```

### Adoption: bring an existing setup under management

```bash
# You already installed claude plugins by hand. Run once:
aide adopt --yes

# config.yaml now lists everything that was installed.
# Subsequent syncs treat them as managed:
aide sync --plan
```
