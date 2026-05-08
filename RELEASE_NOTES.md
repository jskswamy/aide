## v1.10.0

### 🔒 Security

- **Split SSH primitives out of the `git-remote` capability.** Previously,
  enabling `git-remote` (auto-detected from `.git/config` containing
  `[remote `) silently bundled `~/.ssh` read access, `SSH_AUTH_SOCK`
  forwarding, and outbound port 22 — letting an agent push over SSH even
  when the `ssh` capability was not enabled, by leaning on the
  ssh-agent socket forwarded from the host. `git-remote` is now
  HTTPS-only (port 443 + git credential manager). Git-over-SSH requires
  explicit `--with ssh`. Mental model now matches reality: no `ssh`
  capability, no SSH push.

### ✨ New

- **`ssh` capability is now a first-class opt-in guard.** Owns
  `~/.ssh` reads, `SSH_AUTH_SOCK` env passthrough, and outbound SSH
  ports. Use it for `git push`/`fetch` over SSH, `ssh` login,
  `scp`/`rsync` over SSH.
- **Custom SSH ports resolved from four channels** (deny-default;
  port 22 only allowed when SSH is actually in use):
  - `~/.ssh/config` — `Port` directives
  - `.git/config` — `ssh://user@host:PORT/...` remote URLs
  - `AIDE_SSH_PORTS` env (comma-separated; replaces auto-detected set)
  - `.aide.yaml` — `capabilities.ssh.ports: [2222, 2223]`
- **Discoverability hint.** When `git-remote` detects an SSH-style
  remote in `.git/config` but `ssh` is not enabled, the banner shows:
  `💡 git-remote: detected SSH remote(s); enable the 'ssh'
  capability to push/fetch over SSH (aide cap enable ssh)`.
- **`MergedRegistry` now layers user-defined capabilities onto
  builtins** instead of replacing them — so `.aide.yaml` can extend
  `ssh` with `ports:` without re-declaring readables/env.

### ⚠️ Breaking

- Sessions that relied on `git-remote` to grant SSH access must now
  also enable the `ssh` capability. The first run after upgrade
  surfaces the hint above to make the migration discoverable.
