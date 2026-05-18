## Unreleased

### ✨ Features

- **Declarative agent provisioning.** Declare plugins and MCP servers
  per context in `config.yaml` and reconcile them against the agent's
  installed state with a single command. Replaces hand-rolled,
  per-machine bootstrap with a Terraform-style plan-then-apply
  workflow that works end-to-end for Claude, Copilot, Codex, and
  Gemini.

  The schema is **polymorphic by YAML value shape** — no `type:`
  discriminator, the reader sees what each entry means:

  ```yaml
  plugins:
    steveyegge/beads: [beads]                  # list  = marketplace + plugins
    jskswamy/claude-plugins: [craft, devenv]   # list  = marketplace + plugins
    gemini-cli-tool: "github:google/foo"       # string = URL-direct (Gemini)
    obra/superpowers-marketplace: ~            # null  = declare-only

  mcp_servers:                                 # always-inline (one shape)
    postgres: { command: postgres-mcp, args: ["--port", "5432"] }
    rfctl:    { command: rfctl, args: [serve] }

  contexts:
    default:
      agent: claude                            # inherits everything top-level
    work:
      agent: claude
      env: { CLAUDE_CONFIG_DIR: ~/.claude-work }
      plugins:
        exclude:                               # subtract from inherited
          - obra/superpowers-marketplace/double-shot-latte
        extra:                                 # add on top
          my-org/internal: [private-tool]
  ```

  Per-context `plugins:` and `mcp_servers:` blocks accept three
  delta keywords with deterministic composition: `only` (replace
  defaults), `exclude` (subtract), `extra` (add). Path syntax
  `repo/plugin` reaches inside a marketplace entry — e.g. `exclude:
  [jskswamy/claude-plugins/devenv]` removes one plugin without
  touching the rest.

  Four CLI commands wire the workflow through:

  - **`aide sync`** — plan-then-apply reconciliation. `--plan` to
    preview, `--yes` for non-interactive runs. Marketplace adds are
    sequenced before plugin installs. Each successful op records an
    inverse in an in-memory journal; on any failure the engine walks
    the journal in reverse and rolls back, then prints the failing
    op and a retry hint. State is persisted to
    `~/.local/state/aide/managed.json` atomically and only on
    success.
  - **`aide adopt`** — promote agent-installed but undeclared items
    into config.yaml. Marketplace agents get nested list-valued
    entries under the right repo key (looked up via the driver's
    `InstalledMarketplaces`); URL-direct agents get string-valued
    entries.
  - **`aide plugin list`** — three-column declared / installed /
    managed view per context. For marketplace agents the output
    includes a MARKETPLACES section first, surfacing the agent's
    canonical marketplace name (e.g. `beads-marketplace` for
    `steveyegge/beads`) and flagging installed-but-undeclared
    marketplaces as `unmanaged`.
  - **`aide mcp list`** — same shape for MCP servers.

  **Launch-time drift hint.** A one-line banner appears under `aide
  which` when the active context is out of sync. Drift is per-context:
  each context records its own `config_hash` + `synced_at` in
  `managed.json`, so a successful sync of one context never silences
  the banner for another. Two cheap signals fire the banner: (1) the
  context's recorded hash differs from the current `config.yaml`
  hash, or (2) the desired set computed for the context has items
  not yet recorded as managed (shortfall). Both stay in-process — no
  agent-state polling at launch.

  **Multi-profile correctness.** Drivers honor per-context env
  (`CLAUDE_CONFIG_DIR`, `GEMINI_HOME`, …) when invoking the agent
  CLI, so contexts pointing at different agent profiles target the
  right one. The seatbelt rules emitted for an override profile use
  the absolute path (tilde-expanded) — earlier paths in literal
  `~/...` form silently never matched the syscalls the agent makes.

  **Architecture.** New `internal/provision/` package: `Provisioner`
  interface with capability flags (`SupportsPlugins`, `SupportsMCP`,
  `RequiresTTY`, `SupportedSourceShapes`) and marketplace ops
  (`InstalledMarketplaces`, `AddMarketplace`, `RemoveMarketplace`).
  Plan computation diffs desired ∩ installed ∩ managed. Two-phase
  Apply: marketplaces first, then plugins (rewriting each plugin's
  `<plugin>@<repo>` ref to the agent's canonical
  `<plugin>@<marketplace-name>` via a lazy cache invalidated on
  every marketplace op), then MCP servers. MCP file format is
  pluggable per driver: `jsonflat` (Gemini, Copilot), `claudejson`
  (Claude — handles flat `.mcp.json` and the nested
  `projects.<path>.mcpServers` form in `~/.claude.json`),
  `codextoml` (Codex `[mcp_servers.<name>]` tables). A `Runner`
  interface decouples subprocess execution so driver tests don't
  need real agent binaries.

  **Out of scope for this cut.** Goose, Amp, and Aider provisioning
  (tracked separately); project-scope `.aide.yaml` plugin/MCP
  merging into `aide sync` (filed as a feature follow-up); the
  user-friendly `profile: <name>` field that would let users declare
  a profile name and have the driver compute the right env var
  (also a feature follow-up).

  **Bootstrap proof.** End-to-end smoke verified on a clean Claude
  profile (`~/.claude-bootstrap-test`, zero marketplaces, zero
  plugins): declare 2 marketplaces + 9 plugins, run `aide sync --yes`,
  17 seconds later all marketplaces are added, all plugins
  installed, state populated. `claude plugin list --json` confirms
  parity. The full bootstrap loop in one command.

  Spec: `docs/specs/2026-05-15-declarative-agent-provisioning-design.md`.
  Capability research: `docs/specs/2026-05-16-agent-capability-research.md`.

### 🧹 Internal

- **`provision.SourceRef` centralises marketplace ref parsing.** The
  `github:` / `git:` / `https://` / `http://` / leading-`/` prefix
  vocabulary previously lived in three places (`keyAsSource`,
  `classifySource`, `normalizeMarketplaceRef`). One canonical type
  now owns it with `.Aide()`, `.Bare()`, and `.Classify()` methods
  and a table-driven test covering every transport. Adding a new
  scheme is a one-file change.
- **`mcp.jsonFlat.Write` calls existing `reconcile()` helper.** The
  preserve-unmanaged + drop-old-managed + marshal-desired + sort
  algorithm was inlined alongside the already-extracted
  `reconcile()` used by `claudeJSON`. Routing both writers through
  the shared helper cuts ~20 duplicated lines and removes drift
  risk when the algorithm hardens.
