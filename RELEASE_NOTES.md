## v3.4.1 (2026-11-06)

### 🐛 Bug Fixes

- Emit 2-space indented YAML across all config writers (`config.yaml`, `sandbox.yaml`, `provisioning.yaml`, `profiles/<name>.yaml`, and `.aide.yaml`), preserve comments, `---` headers, and key order when updating existing YAML files, and skip writing split files whose semantic contents did not change.
- Resolve unisolated profile config directories across `aide gateway`, `aide which`, startup banner enrichment, and launch-time gateway sync through the harness sandbox module's `EnvProvider` overrides (such as Claude's `~/.config/aide/claude` redirect on Linux) and profile `env:` overrides before falling back to the harness default directory.
