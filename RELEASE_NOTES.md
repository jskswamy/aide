## v1.13.0 — 2026-05-19

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

- **MCP env values resolve secrets at sync time.** Declare an MCP
  server that needs a credential the same way you declare any other
  templated value in `aide`:

  ```yaml
  mcp_servers:
    github:
      command: github-mcp-server
      env:
        GITHUB_TOKEN: "{{ .secrets.github_token }}"

  contexts:
    work:
      agent: claude
      secret: personal       # → personal.enc.yaml (age-encrypted)
      mcp_servers: [github]
  ```

  `aide sync` decrypts the context's `.enc.yaml`, resolves every
  `{{ .secrets.X }}` reference in the MCP servers' `env:` maps, and
  writes the resolved values into the agent's native MCP config
  (`.mcp.json`, `~/.claude.json` projects entry, or Codex `[mcp_servers.<name>]`
  table). Plain string values still pass through unchanged. The
  template engine is the same one launcher.go already uses for
  context env — same `{{ .secrets.X }}` syntax, same
  `missingkey=error` semantics — so the secrets infrastructure is
  unified across both code paths.

  Plan output (`aide sync --plan`) stays safe: only op kind + server
  name is printed, never env values, so secrets never leak to stdout
  / journal / state files. Two failure modes fail loudly at sync
  time (not at agent runtime): a referenced secret missing from
  the `.enc.yaml` errors with the offending key named; a template
  reference in a context without a `secret:` field errors with the
  offending MCP server named.

- **First-class agent profile support.** Multi-profile contexts can
  now declare `profile: <name>` instead of hand-rolling agent-specific
  env vars. The driver computes the right env var and absolute config
  path; users don't need to memorize `CLAUDE_CONFIG_DIR` vs
  `GEMINI_HOME` vs `CODEX_HOME` vs `COPILOT_HOME`:

  ```yaml
  contexts:
    work:
      agent: claude
      profile: work        # → CLAUDE_CONFIG_DIR=~/.claude-work
  ```

  Optional `profile_dir: <abs-path>` overrides the derived
  `~/.<agent>-<name>` path. `cursor-agent` is intentionally not
  supported — its `CURSOR_CONFIG_DIR` env var doesn't isolate
  `mcp.json`; use project-scoped `.cursor/mcp.json` for per-project
  MCP instead. Existing configs with explicit env vars (e.g.
  `env: { CLAUDE_CONFIG_DIR: ~/.claude-other }`) keep working
  unchanged. Declaring both `profile:` and the matching env var in
  the same context is a config-load error.

  Spec: `docs/specs/2026-05-18-agent-profile-design.md`.

### 🐞 Bug Fixes

- **Custom capability paths resolve symlinks for sandbox rule emission.**
  Closes the same kernel-resolved-path gap that `Allow symlinked dotfiles
  in sandboxed agents` fixed for built-in agent modules, but for the
  *capability* and *raw-config* path inputs that bypass the
  `configDirRules` helper. Before, a custom capability declaring
  `readable: ["~/.config/bd/config.yaml"]` against a home-manager
  installation would emit a literal allow rule for the symlink path while
  the kernel matched against the chain's final target (typically a file
  inside `~/nixos-config/...`), producing a confusing `EPERM` despite the
  banner showing `✓ beads`. The filesystem guard now emits both the
  literal and the EvalSymlinks-resolved rule when the resolved target
  lives under `$HOME`; outside-`$HOME` targets are intentionally NOT
  widened here — that's reserved for the upcoming `extra_allow_outside_home`
  escape hatch so it stays a deliberate user opt-in. Deny rules
  (`ExtraDenied`, `never_allow`, `aide-secrets`) resolve symmetrically and
  *without* the `$HOME` gate, since over-denying has no security downside
  and asymmetric resolution would let a symlink-fronted secret stay
  writable through the link. Cycle-bearing capability paths are now
  detected at config load (via `filepath.EvalSymlinks`/`fsutil.IsSymlinkCycle`)
  and surface a clear `capability "X": readable path "Y": symlink cycle
  detected` error rather than degrading silently. `aide cap show <name>`
  annotates each `Readable`/`Writable`/`Deny` entry with its resolved
  target (`declared  →  resolved`) and marks targets outside `$HOME`
  with `⚠ outside $HOME` plus a pointer to the escape hatch, so users
  can audit what the sandbox actually grants before launching a session.

- **Cursor sandbox follows symlinks on install and logs dirs.** The
  cursor-agent module emits subpath allow rules for the resolved
  binary's `versions/<ver>/` and sibling `logs/` directories.
  `cursorActiveInstallDirs` already calls `filepath.EvalSymlinks` on
  the binary itself, which resolves any symlinked *parent* dir
  (e.g., `/Applications/Cursor.app → /Volumes/...`). But if `logsDir`
  or `activeVerDir` is *itself* a symlink (rare — user redirects
  logs to an external volume), the literal subpath rule wouldn't
  match the kernel-resolved write target and the sandbox would deny
  writes. The module now applies `fsutil.ResolveOrSelf` to both
  derived dirs before emitting rules. Defensive — closes a narrow
  gap left after the broader sandbox-symlink fix, which only routed
  config-dir paths through the symlink-resolving emitter.

- **Atomic writes for `aide init` and `aide secrets create`.** Three
  user-facing write paths — `aide init` writing `config.yaml`, `aide
  init --force` writing the `.bak` backup, and `aide secrets create`
  (via `(*Manager).CreateFromContent`) writing the encrypted secrets
  file — used raw `os.WriteFile`. A crash between truncate and the
  final byte left a partial file: garbage YAML at best, an
  undecryptable AES blob at worst. All three now go through
  `fsutil.AtomicWrite` (tmp + rename), matching the durability
  guarantees `aide adopt`/`aide sync`/`aide secrets edit` already had.
  Symlink-preservation comes along for free via the same helper.

- **Sandbox rules follow symlinks for dotfiles-managed config.** macOS
  seatbelt fires `file-write*` policy on the kernel-resolved path, not
  the literal syscall argument. Empirically verified: a
  `(subpath "/tmp/sbA")` allow rule does not cover writes whose path
  resolves outside `/tmp/sbA`. So when home-manager/stow/chezmoi
  symlinked either an agent's whole config dir (e.g. `~/.claude →
  ~/dotfiles/claude/`) or individual files inside it
  (`~/.config/aide/config.yaml → ~/dotfiles/aide/config.yaml`) into a
  git repo, sandboxed agent writes through that path were silently
  denied by the kernel. `configDirRules` now resolves each canonical
  config dir via `filepath.EvalSymlinks`, walks it at depth 1, and
  emits additional subpath allow rules for any safe symlink target
  (under `$HOME`, not under sensitive dirs like `~/.ssh`/`~/.aws`/
  `~/.gnupg`). File-level symlinks allow-list the *parent directory*
  of the target so atomic-write tmp+rename siblings — the exact
  pattern in #12 — also work. Dotfiles placed outside `$HOME` (e.g.
  `/Volumes/Repos/dotfiles`, `/opt/...`) are not widened automatically;
  users with that layout can grant access via a custom capability
  (`aide cap create`) rather than relying on the default policy.
  Affects every agent: claude, copilot, codex, gemini, cursor-agent,
  and any future simple-agent driver.

- **Atomic writes preserve symlinked config and secrets files.** When
  `~/.config/aide/config.yaml` (or an encrypted secrets file) was a
  symlink into a dotfiles repo, every write that went through
  `fsutil.AtomicWrite` — `aide adopt`, `aide sync`, `aide context
  create`/`bind`, `aide secrets edit`, `aide secrets rotate` — replaced
  the symlink entry with a regular file. The repo file silently stopped
  receiving updates and lost git history. `AtomicWrite` now resolves
  symlinks via `filepath.EvalSymlinks` before computing the temp-file
  directory and rename target, so the rename swaps the underlying
  file's inode and leaves the symlink intact. Non-symlink installs are
  unaffected. Note: writes still land at 0o600 — repos that previously
  held the file at 0o644 will see it tighten on first write. Reported
  in jskswamy/aide#12.

- **Launcher tilde-expands env values before exec.** `env:
  CLAUDE_CONFIG_DIR: ~/.claude-work` in `config.yaml` previously
  reached the child agent as the literal string `~/.claude-work`.
  Agents don't expand `~` themselves, so claude fell back to project
  scope and reported no user plugins (while `aide plugin list` —
  which already tilde-expands — showed all 20). `Launcher.Launch`
  now runs each resolved value through `homepath.Expand` after
  template substitution, matching the provisioning path.

### 🧹 Internal

- **Symlink test scaffolding consolidated into `internal/testutil`.**
  Five tests across three packages independently spelled out the
  "mkdir parent, write target, mkdir link parent, symlink declared →
  target" sequence — once per file, sometimes more, with subtle
  variations in perms, leaf type (file vs directory), and hop count.
  Three helpers now carry the contract: `MakeSymlinkedFile` (2-hop,
  file leaf), `MakeSymlinkedDir` (2-hop, directory leaf, for tests
  exercising aide-secrets-style scenarios), and `MakeSymlinkChain`
  (N-hop, reproduces the home-manager `~/.config → /nix/store → ~/repo`
  pattern). Saves ~100 lines of test scaffolding across
  filesystem / aide-secrets / cap-show tests and ensures future
  changes (e.g. permissions tightening, dir-typed leaves elsewhere)
  only need to touch one place.

- **Parameterize the symlink-cycle validators in the capability layer.**
  `validateNoSymlinkCycles` and `validateNeverAllowNoCycles` shared a
  single inner loop and differed only in the error-prefix string. A new
  helper `checkCyclesIn(paths, wrapErr)` carries the iteration once;
  each caller supplies a one-line `wrapErr` closure for its own
  diagnostic context (capability name + field, never_allow origin).
  The never_allow cycle path was previously untested — added a regression
  test that locks the wrapping contract before the refactor and keeps
  passing afterward.

- **Unify symlink-widening resolution in `fsutil.ResolveWidening`.**
  The seatbelt filesystem guard and `aide cap show` independently
  composed EvalSymlinks + an under-`$HOME` classifier and the
  compositions had already drifted (the guard used `ResolveOrSelf`
  with no tilde handling, cap show called `EvalSymlinks` directly
  after `homepath.Expand`). When the next safety-floor change lands
  (the escape hatch for outside-`$HOME` paths), only one site would
  have been updated and the audit display would silently diverge
  from the actual sandbox emission. Both callers now route through
  one helper that returns `(resolved, changed, underHome)`; each
  picks its own response — the guard drops outside-`$HOME` targets,
  cap show annotates them with a warning. The helper stays
  filesystem-pure (no `homepath` dependency), matching the
  convention `ResolveOrSelf` and `CheckSymlinkCycle` already
  established.

- **Move symlink-cycle check into `fsutil.CheckSymlinkCycle`.** The
  capability-layer `checkSymlinkCycle` helper composed three concerns
  (tilde-expand a user path, run EvalSymlinks, translate ELOOP into a
  sentinel error) and lived in `internal/capability/capability.go`
  where only the first concern is capability-specific. The
  filesystem-primitive parts now live alongside `ResolveOrSelf` and
  `IsSymlinkCycle` in `internal/fsutil` as `CheckSymlinkCycle(path)`,
  taking an already-resolved path. The capability layer keeps a
  one-line `capabilityCheckCycle` adapter that applies the
  `homepath.Expand` convention before delegating. The pure-filesystem
  helper is now reusable from any package validating user paths
  (notably the `aide cap show` audit view tracked in a follow-up).
  Four sub-tests cover the cycle / missing / regular-file / non-cycle
  chain cases.

- **Consolidate "is path under dir" predicate into `fsutil.IsUnderDir`.**
  Five sites independently implemented the separator-aware prefix check
  used to gate sandbox widening, prefix-closed sets, and sensitive-dir
  rejection: `pathUnder` (cmd/aide/cap.go), `isUnderDir`
  (pkg/seatbelt/guards/guard_filesystem.go), `isUnderHome` and two
  inline `HasPrefix(... + sep)` expressions (pkg/seatbelt/modules/helpers.go).
  All now route through one helper in `internal/fsutil` with inclusive
  `path == dir` semantics. The one site that required strict-under
  semantics (refusing to widen a sandbox rule to `$HOME` itself, inside
  `isSafeConfigOverride`) now expresses the constraint with an explicit
  `dir != home` guard at the call, so the strict-vs-inclusive
  distinction is visible at the point of use rather than buried in a
  missing-separator subtlety. Pre-existing behavior preserved across all
  call sites; eleven sub-test cases cover equality, similar-prefix false
  positives, trailing separators, and empty inputs.

- **Typed seatbelt rule builders replace ad-hoc `fmt.Sprintf` sites.**
  `seatbelt.AllowSubpath` / `DenySubpath` / `AllowLiteral` /
  `DenyLiteral` take `(path, ops...)` and use `%q` quoting under the
  hood, so paths containing `"` or `\` are escaped into valid sexp
  instead of breaking the surrounding string. Five sites migrated
  (`guards/helpers.go` DenyDir/DenyFile/AllowReadFile,
  `guards/guard_git_remote.go` git-credentials deny,
  `guards/guard_project_secrets.go` hooks-dir deny, `path.go`
  SubpathWithParentMetadata, `modules/helpers.go` configDirRules
  emit). Complex multi-path / network / require-any rules still use
  `fmt.Sprintf` and are tracked separately for a richer builder.

- **`provisiontest.FakeProvisioner` consolidates the two hand-rolled
  Provisioner fakes.** Both `internal/provision/engine_test.go` and
  `cmd/aide/provision_list_test.go` defined their own
  13-method test double in different styles and had already drifted
  (field names, recording conventions, error-injection points). One
  shared fake now lives in `internal/provision/provisiontest`,
  supporting both the unified call-log style and per-method slices.
  Adding a new `Provisioner` method now requires updating exactly
  one file.
- **`provision.RunCLI` unifies driver shell-out plumbing.** Three
  drivers (claude/copilot/gemini) repeated the same wrap-err /
  wrap-exit / tolerate-stderr shape across ~12 Install / Uninstall /
  AddMarketplace / RemoveMarketplace methods. They now route through
  one helper with a shared `DefaultTolerateStderr` constant for the
  rollback-safety substrings. Codex is intentionally unchanged
  (it edits TOML directly).
- **`provision.DriverBase` collapses per-driver capability stubs.**
  All four drivers (claude/copilot/codex/gemini) carried the same
  five trivial methods (`Name`, `SupportsPlugins`, `SupportsMCP`,
  `RequiresTTY`, `SupportedSourceShapes`). A new
  `provision.Capabilities` struct + embeddable `DriverBase`
  promotes them from a single populated literal in each driver's
  `New()`. Adding a new capability bit is a one-file change.
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
