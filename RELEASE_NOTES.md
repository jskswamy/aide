## v3.1.0 (2026-10-21)

### ✨ New

#### Support for Antigravity (`agy`)

aide now discovers, sandboxes, and launches Google Antigravity CLI (`agy`).

- Auto-discovered on PATH under canonical name `agy` (with alias `antigravity`).
- Sandbox access granted to `~/.gemini` (including `~/.gemini/antigravity-cli` and `~/.gemini/config`).
- `--yolo` maps to Antigravity's `--dangerously-skip-permissions` flag.

#### A built-in ponytail capability

When ponytail is set up (`~/.config/ponytail` exists, or Skillshare has
it installed), aide now turns on a `ponytail` capability by itself.
Before this, the ponytail band could not record each task and showed
`ponytail band: could not write the task ledger`, because the sandbox
did not let it write `~/.config/ponytail/band`.

- Read/write access to `~/.config/ponytail/band` only.
- Turn it off with `--without ponytail`, or add it by hand with
  `--with ponytail`.
- Default XDG location only; a custom `XDG_CONFIG_HOME` is not followed.

### 🔒 Security

#### Go 1.26.10 resolves standard library vulnerability in crypto/x509

govulncheck reported GO-2026-6809 (compromised intermediate certificates
in crypto/x509) in Go 1.26.9.

- Updated Go toolchain to 1.26.10 in go.mod.
- Updated Nix development environment and package build to pin Go 1.26.10.

### 🔧 Internal

- Updated Codex yolo mode to use its current `--approve-for-me` flag;
  the obsolete `--full-auto` flag was rejected by current Codex releases.

- Documented the Codex profile-isolation contract and added launcher coverage
  for isolated homes, native login fallback, credential scrubbing, explicit
  credential errors, and native argument passthrough. Runtime admission
  remains pending verification; no runtime behavior changed.
