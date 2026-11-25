## v3.8.0 (2026-11-26)

### ✨ New

- **Built-in `terminal-browser` capability (explicit opt-in)**: `aide --with terminal-browser` (or `capabilities: [terminal-browser]`) unlocks `terminal-browser ls`, `action`, and `open` inside the sandbox. It dynamically resolves existing `terminal-browser-<8-hex>` / `terminal-browser-dev-<8-hex>` XDG data, state, and runtime directories in Go for macOS Seatbelt and Linux Landlock parity, keeps config read-only, validates env overrides against sensitive and system directories, and on macOS emits scoped Unix-socket rules (`network-outbound` + `network-bind` for hashed state, runtime, and interop dirs), `signal (target others)` for host-instance liveness checks, and `localhost:*` TCP for Chromium CDP. It requires explicit opt-in because macOS cannot scope host signaling or ephemeral localhost CDP ports to a single process. See [Capabilities](docs/capabilities.md).
- **Embedded YAML JSON Schemas and `aide config schema` CLI**: Added Draft 2020-12 JSON Schemas for `config.yaml`, `profiles/*.yaml`, `sandbox.yaml`, `provisioning.yaml`, and `.aide.yaml` (embedded in the binary and published in release archives and GitHub Release assets). `aide` now injects and preserves `# yaml-language-server: $schema=...` modelines on write, and provides `aide config schema show`, `export`, and `stamp` (with `--local`, `--force`, and `--project`) for offline and existing-file workflows.
- **Claude gateway model picker with pricing**: Claude Code gateways now write per-gateway `modelPicker` settings (`<claude-dir>/cache/aide-gateway-<gw>.json`) passed via `--settings`, preserving all synced gateway models verbatim without overwriting `settings.json` and appending `$in/$out per Mtok` pricing to model descriptions when returned by `/v1/models`.

### 🐛 Bug Fixes

- Preserve non-credential `Capability.EnvAllow` keys (such as `XDG_DATA_HOME`, `XDG_STATE_HOME`, and `TERMINAL_BROWSER_*`) across `clean_env: true` filtering in both `launcher` and `sandbox`, while continuing to strip credential-named variables even under `--no-sandbox --clean-env`.
- Switch diagnostic report filename suffix hashing from SHA-256 to 32-bit FNV-1a.
