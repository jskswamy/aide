## Unreleased

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
  built aide binary using standalone Node. Linux verification remains pending.

### 🔧 Internal

- Preserve agent profile-directory environment variables under
  `sandbox.clean_env`, including `OPENCODE_CONFIG_DIR`, `PI_CODING_AGENT_DIR`,
  `CODEX_HOME`, and `CLAUDE_CONFIG_DIR`, so launches retain their configured
  profile instead of falling back to global state.

- Resolve gateway lint findings without changing command behavior; check the
  rotating-token test's type assertion and format its test cases.

- Reduce `AGENTS.md` to the project's beads issue-tracking notice.
