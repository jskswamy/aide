## Unreleased

### 🔧 Internal

- Preserve agent profile-directory environment variables under
  `sandbox.clean_env`, including `OPENCODE_CONFIG_DIR`, `PI_CODING_AGENT_DIR`,
  `CODEX_HOME`, and `CLAUDE_CONFIG_DIR`, so launches retain their configured
  profile instead of falling back to global state.
