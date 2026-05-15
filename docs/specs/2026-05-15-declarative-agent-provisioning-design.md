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

## Schema

### Top-level

```yaml
# Existing
mcp_server_overrides:
  postgres:
    command: postgres-mcp
    args: ["--port", "5432"]
  github:
    command: github-mcp
    args: ["--port", "9090"]

# New
plugins:
  linear:
    source: marketplace      # one of: marketplace | git | local
    name: linear@1.2          # agent-interpreted reference
  github:
    source: marketplace
    name: github
  internal-tool:
    source: git
    name: github.com/my-org/internal-tool@v0.4

contexts:
  work:
    agent: claude
    plugins: [linear, github, internal-tool]   # references by name
    mcp_servers: [postgres, github]            # already supported
```

### `plugins.<name>.source` enum (validated at parse)

- `marketplace` — agent's built-in plugin marketplace (e.g. Claude Code
  marketplace). `name` is whatever the agent's CLI accepts.
- `git` — a git URL (with optional `@ref`). The agent driver translates
  this to the agent's git-install command if supported, else errors at
  sync.
- `local` — a filesystem path. Same translation rule.

Unknown sources fail at config-load time with a clear error and the list
of supported values.

### Reference resolution

`contexts.<x>.plugins: [linear]` references `plugins.linear` by key. If
the referenced name is missing from the top-level `plugins:` map, config
load fails with `context "x" references undefined plugin "linear"`.

### Capability mismatch

If `contexts.<x>.agent: aider` (which does not support plugins) and
`contexts.<x>.plugins` is non-empty, `aide sync` errors with:

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

    // MCP — implemented by shared helpers since the file format is uniform.
    MCPConfigPath(ctx Context) string
    InstalledMCP(ctx Context) (map[string]MCPServer, error)
    WriteMCP(ctx Context, desired map[string]MCPServer) error

    // Plugins — agent-specific shell-out.
    InstalledPlugins(ctx Context) ([]Plugin, error)
    InstallPlugin(ctx Context, p Plugin) error
    UninstallPlugin(ctx Context, name string) error
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

To be filled in during implementation by reading each agent's docs and
testing. Current best-effort assessment:

| Agent       | Plugins | MCP | Notes                                |
| ----------- | ------- | --- | ------------------------------------ |
| Claude Code | yes     | yes | marketplace + .mcp.json              |
| Goose       | yes     | yes | "extensions" treated as plugins      |
| Codex       | ?       | ?   | needs verification                   |
| Gemini CLI  | ?       | yes | needs verification                   |
| Aider       | no      | ?   | model + git focus                    |
| Amp         | ?       | yes | needs verification                   |
| Copilot CLI | no      | ?   | recent MCP support, no plugin model  |

Each unknown is resolved during driver implementation. Until verified,
the driver returns `false` for both — fail closed.

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
