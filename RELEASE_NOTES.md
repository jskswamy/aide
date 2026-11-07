## v3.5.0 (2026-11-07)

### 💥 Breaking changes

- Isolate every profile's harness configuration directory by default (`config_dir_name` defaults to the sanitized profile name when omitted) and reject configurations that pin a harness's global default config directory (`~/.claude`, `~/.codex`, `~/.pi/agent`, `~/.config/opencode`, etc.). Remove `--adopt` and global-login sharing on `aide profile create` and `aide setup`.
- Remove legacy flat top-level configuration fields (`agent`, `env`, `secret`, `sandbox`, `yolo` at the root of `config.yaml`). All configurations must use `profiles:` (or legacy `contexts:`).

### ✨ New

- Update `aide init` to generate the modular split configuration layout (`config.yaml` + `profiles/default.yaml`) instead of a single monolithic `config.yaml`.

### 🐛 Bug Fixes

- Mount system baseline paths (including `--tmpfs /tmp`) before policy writable and readable binds in the Linux `bubblewrap` (`bwrap`) sandbox backend so submounts under `/tmp` (such as `RuntimeDir` or temporary project roots) are not masked by the fresh `/tmp` tmpfs.
- Fall back to the kernel `LANDLOCK_CREATE_RULESET_VERSION` syscall probe in `DetectKernelCapabilities` on Linux when `securityfs` (`/sys/kernel/security/lsm`) is not mounted inside containers.

### 🔧 Internal

- Decouple `pkg/seatbelt` (`guards` and `modules`) from `internal/fsutil` and `internal/homepath` by moving `ExpandHome`, `ResolveOrSelf`, `IsUnderDir`, and `ResolveWidening` into `pkg/seatbelt` and enforcing the boundary in `TestNoInternalImports`.
- Unify CLI mutation target resolution across `cmd/aide` commands via `Env.ResolveContextForMutation` and `Env.ResolveProjectOverrideForMutation`.
