# Declarative agent provisioning design

Status: draft
Date: 2026-05-15
Author: Krishnaswamy Subramanian

## Why

A new machine needs the same plugins and MCP servers across the same set
of contexts every time. Today the configuration tells `aide` *which*
plugins and MCP servers belong to each context only for the purpose of
launching the agent — there is no mechanism to install, register, or
reconcile that state against what is actually present in the agent's own
config. Bootstrap scripts cannot describe "context X has plugins A, B and
MCP server C" and apply it to a fresh machine without per-agent manual
steps.

This design adds a declarative reconciliation layer for plugins and MCP
servers, per context, with explicit `aide sync` semantics modelled on
Terraform's plan-then-apply UX.

## Goals

- Declare plugins and MCP servers per context in `config.yaml` once;
  reproduce that state on any machine via `aide sync`.
- Plan-then-apply: every sync prints the diff and asks for confirmation
  unless `--yes` is passed.
- Abort + rollback on partial failure; never leave the agent or aide's
  state file partly updated.
- Cheap launch path: no agent-state polling at startup. A single
  config-hash check feeds a one-line drift banner.
- Coexist with manual experimentation: aide reconciles only what *it*
  installed. User-installed plugins and MCP entries survive.
- Promote manually-installed state into declarative config via an
  explicit `aide adopt` flow.

## Non-goals

- A general-purpose package manager. Aide delegates installation to each
  agent's own CLI where possible.
- Reverse-engineering agent marketplaces. If an agent has no plugin CLI,
  aide reports `SupportsPlugins() == false` and any plugin declaration
  for that agent errors at sync time.
- Automatic drift remediation on launch. The launch banner hints; only
  `aide sync` mutates state.
- Multi-machine sync of installed-state. The state file is local.

## UX

### `aide sync`

```
$ aide sync --context work

Plan for context "work" (agent: claude):

  + plugin    linear            (marketplace, not installed)
  + mcp       postgres          (declared, not registered)
  ~ mcp       github            (command changed: --port 8080 → 9090)
  - plugin    old-tool          (previously managed, no longer declared)
    plugin    github            unchanged
    mcp       postgres-replica  unmanaged (left alone)

Unmanaged items found:

  plugin   experimental-tool   (installed manually, not in config)
  mcp      slack               (installed manually, not in config)

What to do with each?

  experimental-tool  [a]dopt / [i]gnore / [u]ninstall : a
  slack              [a]dopt / [i]gnore / [u]ninstall : i

Proceed with: 1 add, 1 change, 1 remove, 1 adopt? [y/N]: y

  ✓ installed plugin "linear" (marketplace, v1.2)
  ✓ wrote MCP server "postgres" to ~/.claude.json
  ✓ updated MCP server "github" command
  ✓ uninstalled plugin "old-tool"
  ✓ adopted "experimental-tool" → contexts.work.plugins
  ✓ wrote state to ~/.local/state/aide/managed.json

Sync complete.
```

### `aide sync --yes` (script mode)

Non-interactive. Applies the declared diff:
- declared-but-missing → install
- managed-but-no-longer-declared → uninstall
- unmanaged → **leave alone** (adoption requires a human decision)

Exit code 0 on success, non-zero on failure (with the failure message and
retry hint described under [Failure semantics](#failure-semantics)).

### `aide sync --plan`

Prints the plan and exits 0. Does not prompt and does not mutate.

### `aide adopt`

```
$ aide adopt --context work

Unmanaged items in context "work":
  plugin   experimental-tool   (installed manually)
  mcp      slack               (installed manually)

  experimental-tool  [a]dopt / skip : a
  slack              [a]dopt / skip : a

Writing config:
  + plugins.experimental-tool   (source: marketplace, name: experimental-tool@1.0)
  + mcp_server_overrides.slack  (command, args, env from agent config)
  + contexts.work.plugins ← experimental-tool
  + contexts.work.mcp_servers ← slack

Marking as managed in ~/.local/state/aide/managed.json.

Adoption complete.
```

`aide adopt` only walks unmanaged items. It does not install or remove
anything in the agent. It mutates `config.yaml` (atomic write via
`internal/fsutil.AtomicWrite`) and the state file.

`aide adopt --context work --yes` adopts every unmanaged item with no
prompting — useful for one-shot migration from "I configured everything
by hand" to "now it's declarative".

### Launch drift banner

A single file-stat at launch compares the SHA-256 of `config.yaml`
against `config_hash` in the state file:

- match (common case) → silent
- mismatch → one-line banner under the existing aide preamble:

```
⚠ context "work": config changed since last sync — run `aide sync`
```

The banner never blocks the launch. The check is one `os.ReadFile` + one
hash on a small YAML; no subprocess to the agent.

## Schema (v2 — polymorphic plugins + delta semantics)

> **v1 historical note.** An earlier iteration shipped a flat
> `plugins: {name: {source, name}}` map. Smoke-testing against a real
> Claude installation revealed three structural issues — marketplaces
> are a distinct concept aide had to model, "plugin" meant materially
> different things across agents, and contexts repeating the same plugin
> list violated DRY badly enough that 18-plugin configs grew to 54
> duplicated lines. v2 replaces that flat shape with what's described
> below. Migration is a one-time config rewrite.

### Two top-level blocks

```yaml
plugins:           # polymorphic-by-value-shape (see below)
  ...

mcp_servers:       # always-inline (one shape)
  ...

contexts:
  <name>:
    plugins:       # references with optional extra/exclude/only deltas
    mcp_servers:   # same delta semantics
```

### `plugins:` — polymorphic by YAML value shape

The block is a single map. Each entry's *value type* tells aide what
kind of source it represents:

| YAML value type | Semantic | Key role | Driver class |
|---|---|---|---|
| **List of strings** | Marketplace with N plugins inside | repo path (`owner/repo`) | Marketplace (Claude/Copilot/Codex) |
| **String** | Single install reference (URL or local path) | plugin name | URL-direct (Gemini) |
| **`null`** | Declare-only (e.g. register a marketplace, install nothing from it) | repo path | Marketplace |

There is **no `type:` discriminator field**. The YAML value type is the
discriminator. Reading the block tells you what each entry means at a
glance.

```yaml
plugins:
  # Marketplace entries — list value, key is a repo
  steveyegge/beads:
    - beads
  jskswamy/claude-plugins:
    - craft
    - devenv
    - jot
    - codebase
  anthropics/claude-plugins-official:
    - context7
    - plugin-dev

  # URL-direct entries — string value, key is a plugin name
  gemini-cli-tool: "github:google/gemini-cli-tool"
  my-local-ext:    "~/src/some-extension"

  # Declare-only — value is null; useful to register a marketplace
  # without (yet) installing anything from it
  obra/superpowers-marketplace: ~
```

### `mcp_servers:` — always inline

MCP servers are not bundles you "install" — they're processes the agent
spawns and talks to. Every agent that supports MCP uses the same
conceptual fields: `command` + `args` + `env` (or `url` + `headers` for
remote servers). File-format differences (JSON / TOML / YAML / dotted
keys) are driver-level concerns.

```yaml
mcp_servers:
  rfctl:
    command: rfctl
    args: [mcp-server]
  postgres:
    command: postgres-mcp
    args: ["--port", "5432"]
  slack:
    url: https://mcp.slack.app/sse
    headers:
      Authorization: "Bearer ..."
```

Conflating an MCP server entry with a plugin entry would be a category
error: a plugin is an installable bundle; an MCP server is a runtime
process. They get separate blocks.

### Per-context references with `extra` / `exclude` / `only` deltas

A context with no `plugins:` block inherits all top-level entries
verbatim. To customise, contexts use three keywords:

| Keyword | Meaning |
| --- | --- |
| *(no block)* | Inherit all top-level entries. |
| `exclude: [<path>...]` | Start from inherited defaults, then remove. Path syntax: `repo` removes the whole marketplace; `repo/plugin` removes one plugin from a list-valued entry; `key` removes a string- or null-valued entry. |
| `extra: {<key>: <value>}` | Add to (or merge into) inherited defaults. Same value shapes as top-level. Repos already in the master list have their plugin lists *merged* additively. |
| `only: [<path>...]` | Bypass defaults entirely; the context's set is exactly what's listed. Same path syntax as `exclude`. |

**Composition order** (deterministic, no surprises):
1. Start with the top-level master map. If `only:` is present, replace
   that start with the explicit list.
2. Apply `exclude:` — remove entries by key or by `repo/plugin` path.
3. Apply `extra:` — add or merge.

`only` may coexist with `exclude` / `extra`; order makes it useful for
"these specific plugins + one more, minus one I don't want."

Same semantics apply to `mcp_servers:`. Path syntax for MCP excludes is
just `key` — MCP servers don't nest.

```yaml
contexts:
  default:
    agent: claude
    # Inherits all plugins and mcp_servers from top-level

  prod:
    agent: claude
    env: { CLAUDE_CONFIG_DIR: ~/.claude-prod }
    plugins:
      extra:
        my-org/internal: [private-tool]
      exclude:
        - obra/superpowers-marketplace/double-shot-latte
    mcp_servers:
      extra:
        rfctl-work:
          command: rfctl
          args: [serve, --env, work]

  ci:
    agent: claude
    plugins:
      only:
        - jskswamy/claude-plugins
        - jskswamy/claude-plugins/craft   # keep only this plugin from that marketplace
```

### Validation

**Parser-time (config load):**
- Key-vs-value-shape consistency for `plugins:` entries:
  - List value → key must look like `owner/repo` (or a valid git URL).
  - String value → value must parse as `github:`, `git:`, `https://`,
    or an absolute local path.
  - Mismatch fails with a clear "key syntax suggests marketplace but
    value is a string source ref" message.
- `exclude:` / `only:` paths must reference declared entries (after
  considering `extra:` additions).
- `extra:` value shapes follow the same key/value-shape rules.

**Sync-time:**
- Each context's agent must accept at least one of the shapes its
  referenced entries use. If a Goose context references a marketplace
  entry (list-valued), sync errors with `agent "goose" does not consume
  marketplace-style plugin entries; see <issue-url> for the inline
  alternative`.

### Driver capabilities

Each driver advertises which shapes it consumes:

```go
type Provisioner interface {
    // ... existing methods
    SupportedSourceShapes() []SourceShape   // {Marketplace, URLDirect}
}
```

| Agent | Plugin shapes consumed | MCP supported? |
|---|---|---|
| Claude / Copilot / Codex | `Marketplace` | yes |
| Gemini | `URLDirect` | yes |
| Goose | (none) | yes — Goose extensions ARE MCP servers; declared in `mcp_servers:`, no `plugins:` for Goose contexts |
| Amp | `URLDirect` (TS file URL/path) | yes |
| Aider | (none) | no |

For marketplace drivers, sync runs in two phases: first ensure each
referenced marketplace exists in the agent's local cache (calls
`marketplace add` if missing); then install each declared plugin from
the now-cached marketplace. The marketplace-name discovered post-add
(e.g. `beads-marketplace` for `steveyegge/beads`) is cached in the
state file so future syncs skip the discovery query.

### Capability mismatch

If `contexts.<x>.agent: aider` (which supports neither plugins nor MCP)
and the resolved set of plugins or MCP servers is non-empty, `aide
sync` errors with:

```
context "x" declares plugins, but agent "aider" does not support plugins.
Either remove the plugins list or switch the agent.
```

Validated at sync time, not config-load time — the capability matrix
lives in agent driver code, not the YAML parser.

## Architecture

### New package: `internal/provision`

```
internal/provision/
  provisioner.go     // Provisioner interface + shared types
  mcp.go             // Shared MCP file-write helpers
  state.go           // managed.json read/write
  plan.go            // diff computation: desired vs installed vs managed
  sync.go            // reconciliation engine (plan, apply, rollback)
  claude.go          // Claude Code driver
  goose.go           // Goose driver
  codex.go           // Codex driver
  gemini.go          // Gemini driver
  aider.go           // Aider driver (no plugins, MCP only if applicable)
  amp.go             // Amp driver
  copilot.go         // Copilot driver
```

### Provisioner interface

```go
type Provisioner interface {
    Name() string
    SupportsPlugins() bool
    SupportsMCP() bool
    RequiresTTY() bool

    // SupportedSourceShapes lists which plugin-entry value shapes the
    // driver consumes: Marketplace (list-valued), URLDirect (string-
    // valued). Drivers that don't have a plugin concept (e.g. Goose,
    // Aider) return nil.
    SupportedSourceShapes() []SourceShape

    // MCP — shape is uniform; driver picks an MCPHandler matching its
    // on-disk format (JSON flat / Claude nested / Codex TOML / etc.).
    MCPConfigPath(ctx Context) string
    MCPHandler(ctx Context) MCPHandler

    // Plugins — agent-specific shell-out. Marketplace drivers' install
    // path also handles marketplace-add as a prerequisite.
    InstalledPlugins(ctx Context) ([]Plugin, error)
    InstallPlugin(ctx Context, p Plugin) error
    UninstallPlugin(ctx Context, name string) error

    // Marketplace drivers only — installed marketplaces in this
    // profile, and add/remove operations. URLDirect / inline drivers
    // return nil for InstalledMarketplaces and silently no-op the
    // add/remove calls (engine should never invoke them).
    InstalledMarketplaces(ctx Context) ([]Marketplace, error)
    AddMarketplace(ctx Context, m Marketplace) error
    RemoveMarketplace(ctx Context, name string) error
}
```

### MCP shared path

Each agent driver supplies `MCPConfigPath`. The shared
`WriteMCP` helper:

1. Reads the existing JSON (or returns empty if missing).
2. Replaces only the entries listed in the desired map.
3. Reads the `_aide_managed` array; reconciles aide's owned entries and
   leaves all others untouched.
4. Atomic-writes via `internal/fsutil.AtomicWrite`.

This isolates the one tricky bit (file format) per agent while keeping
the merge logic in one place.

### Plugin per-agent shell-out

Each driver declares its install/uninstall commands as `exec.Command`
templates. Subprocess stdout/stderr is captured for the rollback message.
Non-zero exit aborts the sync.

If a driver returns `SupportsPlugins() == false`, calling any plugin
method panics — sync is expected to short-circuit before reaching there.

## Reconciliation

### Plan computation

For a given context, the planner computes:

1. **desired**: the declared `plugins` and `mcp_servers` for the context,
   resolved through the top-level definitions.
2. **installed**: queried from the agent driver (`InstalledPlugins`,
   `InstalledMCP`).
3. **managed**: the previously-tracked set from `managed.json` for this
   context.

The plan is a list of `Op` values:

| Op        | Condition                                                                                                                        |
| --------- | -------------------------------------------------------------------------------------------------------------------------------- |
| install   | in desired, not in installed                                                                                                     |
| update    | in desired and installed; for MCP, any of `command`/`args`/`env` differ; for plugins, the version-pinned `name` differs from installed |
| uninstall | in managed, not in desired                                                                                                       |
| adopt     | in installed, not in managed (interactive only)                                                                                  |
| ignore    | in installed, not in managed, declined adoption                                                                                  |

### Apply

Ops are executed in order: installs, updates, uninstalls, adoptions.
Each op is recorded to an in-memory journal. On any failure, the engine
walks the journal in reverse and runs the inverse op (install ⇄
uninstall, update ⇄ restore previous bytes, adoption ⇄ remove from
config).

### State file

`~/.local/state/aide/managed.json`:

```json
{
  "version": 1,
  "config_hash": "sha256:9a3f…",
  "synced_at": "2026-05-15T08:50:00+05:30",
  "contexts": {
    "work": {
      "plugins": {
        "linear":  {"installed_at": "2026-05-15T08:50:00+05:30", "version": "1.2"},
        "github":  {"installed_at": "2026-05-15T08:50:00+05:30", "version": "1.0"}
      },
      "mcp_servers": {
        "postgres":  {"installed_at": "2026-05-15T08:50:00+05:30"},
        "github":    {"installed_at": "2026-05-15T08:50:00+05:30"}
      }
    }
  }
}
```

Written via `internal/fsutil.AtomicWrite`. Only updated when sync
succeeds end-to-end — partial state is never committed.

## Failure semantics

`aide sync` is transactional within a single context. The engine
maintains an in-memory journal of completed ops. On any failure:

1. Stop further execution.
2. Walk the journal in reverse, running each op's inverse.
3. Discard the in-flight state file write.
4. Print:

```
Sync failed during: install plugin "linear"
Error:    subprocess `claude --plugin install linear@1.2` exited 1
          stderr: marketplace fetch timed out
Rolled back: 0 prior ops (this was the first operation)

Retry:    aide sync --context work
          (if marketplace remains unreachable, check network and rerun)
```

Rollback uses the same per-op functions as the forward path:

| Forward op  | Inverse                                                     |
| ----------- | ----------------------------------------------------------- |
| install     | UninstallPlugin                                             |
| update      | WriteMCP with the original bytes                            |
| uninstall   | InstallPlugin with the prior version                        |
| adopt       | rewrite config.yaml without the adopted entry               |

If an inverse op itself fails, the engine logs that failure but
continues rolling back the rest. The final error message lists each
inverse failure, surfacing the worst case: "this op partially
remediated, here's what's still out of place, run aide sync again".

## Capability matrix

Verified 2026-05-16 against official docs per agent. Full per-agent
fact sheets in
[`docs/specs/2026-05-16-agent-capability-research.md`](./2026-05-16-agent-capability-research.md).

| Agent       | Plugins | MCP | Plugin install path           | MCP config path                                  | MCP format | Cleanly scriptable          |
| ----------- | ------- | --- | ----------------------------- | ------------------------------------------------ | ---------- | --------------------------- |
| Gemini CLI  | ✓       | ✓   | `gemini extensions ...`       | `~/.gemini/settings.json` (`mcpServers`)         | JSON       | YES (cleanest)              |
| Copilot CLI | ✓       | ✓   | `copilot plugin ...`          | `~/.copilot/mcp-config.json` (`mcpServers`)      | JSON       | YES                         |
| Claude Code | ✓       | ✓   | `claude plugin install` (non-interactive; `list --json` for discovery — verified 2026-05-17, supersedes 2026-05-16 research) | `~/.claude.json` (user) + `.mcp.json` (project)  | JSON\*     | YES — all of install/uninstall/list scriptable |
| Codex       | ✓       | ✓   | TUI / `npx codex-marketplace` | `~/.codex/config.toml`                           | **TOML**   | MCP yes; plugins TUI-only   |
| Goose       | ✓†      | ✓†  | file edit                     | `~/.config/goose/config.yaml`                    | **YAML**   | File-edit only              |
| Amp         | ⚠️      | ✓   | file-drop `.ts`               | `~/.config/amp/settings.json` (`amp.mcpServers`) | JSON‡      | MCP yes; plugins are code   |
| Aider       | ✗       | ✗   | N/A                           | N/A                                              | N/A        | Skip from feature           |

\* Claude user-scope MCP is nested under `projects.<path>.mcpServers`,
not a flat `mcpServers` top-level key — needs a path-aware reader.

† Goose unifies plugins and MCP under a single `extensions:` map.

‡ Amp's top-level key is `amp.mcpServers` (dotted key inside flat
JSON), not `mcpServers` at the root.

### Driver implementation tiers

| Tier | Drivers          | Effort | Reason                                                                                |
| ---- | ---------------- | ------ | ------------------------------------------------------------------------------------- |
| 1    | Gemini, Copilot  | low    | Clean CLI for both plugins and MCP; shared JSON helper works directly.                |
| 2    | Claude, Codex    | medium | Claude needs path-aware user-scope MCP reader. Codex needs TOML handler and plugin install requires TTY (no scriptable surface). Claude's plugin install/list/uninstall are all scriptable (`--json` flag verified). |
| 3    | Goose, Amp       | medium | Format diversity: Goose YAML + unified plugins/MCP; Amp nested MCP key + TS file-drop plugins. |
| —    | Aider            | skip   | No plugin or MCP support. Driver returns `false` for both capabilities.               |

### Interface tweaks suggested by research

- Add `RequiresTTY() bool` capability so `aide sync --yes` can short-circuit drivers whose plugin install path cannot run unattended (Codex), with a clear error instead of a hang. **Update 2026-05-17:** Claude's CLI surface turned out to be fully non-interactive (`claude plugin list --json`, `install`, `uninstall` all scriptable), so Claude's `RequiresTTY()` returns `false`. Codex remains TTY-only for plugin install.
- The shared MCP helper from Task 7 (JSON + flat `mcpServers`) is **one** implementation, not the only one. Move it into a `internal/provision/mcp/` subpackage with siblings: `jsonflat.go` (current), `claudejson.go` (path-aware projects map), `amp.go` (dotted key), `codextoml.go`, `gooseyaml.go`. Each driver picks its MCP handler in its constructor.

## CLI surface

```
aide sync          [--context <name>] [--yes] [--plan]
aide adopt         [--context <name>] [--yes]
aide plugin list   [--context <name>]        # prints declared + installed + managed columns; read-only
aide mcp list      [--context <name>]        # prints declared + installed + managed columns; read-only
```

`aide plugin add/remove` and `aide mcp add/remove` are explicitly *not*
in this design. The declarative-config workflow is: edit `config.yaml`,
run `aide sync`. Convenience wrappers can come later if friction
warrants them.

## Open questions

- Plugin version pinning semantics across agents — not all marketplaces
  speak `@version`. Driver-by-driver decision during implementation.
- Whether `aide sync` should sync *all* contexts when `--context` is
  omitted, or refuse without an explicit `--all`. Lean refuse-without-all
  to avoid surprise multi-context mutation.
- Where the state file lives on non-XDG platforms (macOS uses
  `~/Library/Application Support`?). Default to XDG everywhere; aide
  already does this elsewhere.

## Future work

- Sync-on-config-change daemon (watches `config.yaml`, triggers `aide
  sync` automatically). Deferred — see goal "no surprise mutations".
- Cross-machine sync of `managed.json` via Dolt/git for matched-machine
  bootstraps. Deferred.
- Plan output as machine-readable JSON for CI integration.
