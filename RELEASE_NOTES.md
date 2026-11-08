## v3.6.0 (2026-11-08)

### ✨ New

- Discover all configured `age` recipients across `SOPS_AGE_KEY`, `SOPS_AGE_KEY_FILE`, and default `keys.txt` paths with an interactive recipient picker when multiple keys exist and in-process X25519 key generation when no key is found.
- Prompt interactively on TTY launches when an untrusted `.aide.yaml` file is encountered (`[y] Trust this file / [s] Skip for this launch / [d] Deny permanently`), displaying the requested capabilities, agent, MCP servers, and sandbox overrides before applying or rejecting the file.
- Detect ambient provider API keys in the shell environment when a profile has no active credential binding: print a scrub notice to `stderr` on non-interactive runs, and offer an interactive 4-option onboarding menu on TTY launches (`[1] Save to encrypted secret store & bind to profile`, `[2] Create a new profile for this folder with this key`, `[3] Use for this launch only`, `[4] Strip and continue`).
- Deprecate and hide `aide setup` and `aide use` in favor of `aide init`, `aide profile bind` (including `[N+1] Create a new profile`), and `aide profile create`.

### 🐛 Bug Fixes

- Keep the built-in `default` profile unisolated on native agent directories (`~/.claude`, `~/.codex`, etc.) and automatically synthesize and fall back to `default` on zero-config launches and unmatched directories, fixing the profile isolation regression introduced in v3.5.0.
