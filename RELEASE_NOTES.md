## Unreleased

### 💥 Breaking changes

- Tighten sandbox configuration-directory grants: symlinks no longer widen
  write access into sensitive or protected state, including GitHub credentials,
  aide/Skillshare directories, and other profiles. Broad configuration overrides
  overlapping credential directories no longer receive those grants. On macOS,
  directory symlinks grant access to the resolved target, not its parent.
  Setups that relied on wider access must use safe, dedicated directories.

### 🔧 Internal

- Preserve agent profile-directory environment variables under
  `sandbox.clean_env`, including `OPENCODE_CONFIG_DIR`, `PI_CODING_AGENT_DIR`,
  `CODEX_HOME`, and `CLAUDE_CONFIG_DIR`, so launches retain their configured
  profile instead of falling back to global state.
