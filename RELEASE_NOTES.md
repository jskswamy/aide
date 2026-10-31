## Unreleased

### 💥 Breaking changes

#### OpenCode profiles keep their own snapshots and prompt history

A profile that has selected an OpenCode config dir (`config_dir_name`,
`config_dir` or `config_dirs.opencode`) now launches OpenCode with
`XDG_DATA_HOME=<config-dir>/data` and `XDG_STATE_HOME=<config-dir>/state`.
Snapshots of your projects, which can include uncommitted file contents,
and your prompt history no longer go to directories every profile can
read. The same launches that open the profile's own database get this.

- Commands run by OpenCode's shell tool inherit `XDG_DATA_HOME`. Tools
  that keep data there, such as `direnv` approvals and `uv` tools and
  Python installs, look empty inside OpenCode until set up for the
  profile.
- Nothing is copied or deleted. Earlier snapshots, logs and prompt history
  stay in `~/.local/share/opencode` and `~/.local/state/opencode`; remove
  them yourself if they must not reach other profiles. Undoing an edit
  from an older session may fail.
- Snapshots sit in the config dir, so anything that copies or syncs that
  directory copies them too.
- A `never_allow_env` entry for either variable now stops the launch with
  an error.

## v3.3.0 (2026-11-01)

### 💥 Breaking changes

- Tighten sandbox configuration-directory grants: symlinks no longer widen
  write access into sensitive or protected state, including GitHub credentials,
  aide/Skillshare directories, and other profiles. Broad configuration overrides
  overlapping credential directories no longer receive those grants. On macOS,
  directory symlinks grant access to the resolved target, not its parent.
  Setups that relied on wider access must use safe, dedicated directories.

### ✨ New

- Support the inspected Pi installer-managed layout inside the sandbox, including
  the shared installation access needed by `pi update`, while preserving
  configured per-profile state isolation. Profiles selecting the same executable
  share writable executable code and one upgrade cycle. A profile can modify
  code later run by another profile; separate state directories do not protect
  against this shared-code risk. Invalid or unsafe managed layouts
  and grants that cannot preserve explicit denials stop launch; non-managed
  installs and global-state profiles keep existing behavior. Launcher recognition
  is limited to the inspected installer format. Native unit/race tests and
  macOS Seatbelt fixture checks pass. A real Pi 1.3.0-to-1.4.0 update, RPC
  startup, profile isolation, and upstream lock refusal also passed through the
  built aide binary using standalone Node. Linux sandbox integration for the
  Landlock and bubblewrap backends passes in CI.

### 🐛 Bug Fixes

- Bind gateway model cache freshness and materialization to the gateway's normalized `base_url` across `aide gateway list`, `aide gateway use`, `aide which`, startup banner enrichment, and harness inspection so changing `base_url` never reports or materializes stale models from a previous endpoint.
- Make `--profile` a persistent flag on `aide gateway` with shell tab-completion so both `aide gateway --profile <name> <subcmd>` and `aide gateway <subcmd> --profile <name>` take highest precedence.
- Resolve `aide gateway edit [<name>]` across profiles when `--profile` is omitted (or show a `(profile, gateway, base_url)` picker on a terminal when multiple gateways exist across profiles), print `Editing gateway "<name>" on profile "<profile>"` before interactive prompts, prompt for field edits when `--rotate-token` is passed on a terminal without field flags, offer token rotation when `--url` changes `base_url` on a terminal, and verify updated URL/token pairs against `<base_url>/v1/models` before saving secrets or `config.yaml`.
- Make the bubblewrap sandbox root read-only after mounts so bwrap's auto-created destination parent directories cannot be written into: unmounted private state (e.g. another profile's `auth.json`) was read-denied by absence but the parent directories were writable, letting a sandboxed agent create files in private profile directories. Denied directories now also get a read-only (`0555`) tmpfs overlay instead of an owner-writable one.

### 🔧 Internal

- Preserve agent profile-directory environment variables under
  `sandbox.clean_env`, including `OPENCODE_CONFIG_DIR`, `PI_CODING_AGENT_DIR`,
  `CODEX_HOME`, and `CLAUDE_CONFIG_DIR`, so launches retain their configured
  profile instead of falling back to global state.

- Resolve gateway lint findings without changing command behavior; check the
  rotating-token test's type assertion and format its test cases.

- Reduce `AGENTS.md` to the project's beads issue-tracking notice.

- Upgrade CI to current action versions (CodeQL v4, cache v6, goreleaser v7.3)
  and pin runners to ubuntu-26.04 ahead of the ubuntu-latest migration; run
  pre-commit directly because its wrapper action still targets Node.js 20.

- Cover the Pi test-fixture package and simple-agent env keys so the
  coverage gate stays green.
