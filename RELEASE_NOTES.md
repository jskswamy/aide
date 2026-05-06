## v1.8.1

### 🐛 Fixes

- **macOS age key discovery.** aide now resolves the sops keys file at
  the OS-canonical path (`~/Library/Application Support/sops/age/keys.txt`
  on macOS), matching sops upstream behavior. Previously aide only
  checked `$XDG_CONFIG_HOME/sops/age/keys.txt`, so macOS users without
  a YubiKey or `SOPS_AGE_KEY*` env vars hit "no age identity found"
  even with a valid keys file present. XDG fallback retained on macOS
  for cross-platform setups.
- **Mixed YubiKey + software identities in one keys.txt.** When a
  YubiKey is on `PATH`, aide also surfaces the default `keys.txt` to
  sops via `SOPS_AGE_KEY_FILE`, so software identities in the same
  file decrypt regular `age1...` recipients (not only
  `age1yubikey1...` ones). Caller-set `SOPS_AGE_KEY_FILE` is respected
  and never overridden.

### 🔧 Internal

- New documentation covering age bootstrap per OS, end-to-end Anthropic
  API key wiring via `aide secrets create` / `aide use` /
  `aide env set`, and a multi-account walkthrough that pairs separate
  secret stores with per-context `CLAUDE_CONFIG_DIR`. See
  `docs/secrets.md` and `docs/contexts.md`.
