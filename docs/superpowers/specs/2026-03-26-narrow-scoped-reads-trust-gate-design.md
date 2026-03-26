# Narrow Scoped Reads + Trust Gate for .aide.yaml

**Date:** 2026-03-26
**Status:** Draft

## Problem

The current filesystem guard allows broad read access to home directory
subtrees (`~/.config/*`, `~/.ssh/*`, `~/.gnupg/*`, home dotfiles). Guards
then carve out deny rules for sensitive paths within these broad allows.

This architecture has two problems:

1. **Unknown unknowns are exposed.** Any new CLI tool storing credentials
   under `~/.config/` is readable by default until someone adds a guard.
2. **Redundant guards.** Guards denying paths outside scoped reads (like
   `~/.aws`, `~/.docker`, `~/.kube`) are unnecessary because deny-default
   already blocks them. The guard system carries dead weight.

Additionally, `.aide.yaml` project overrides are applied unconditionally.
A cloned repo can add capabilities, unguard guards, add writable paths,
and set MCP servers without user consent.

## Design

### 1. Minimal Baseline Reads

The filesystem guard's scoped home reads shrink to the bare minimum
needed for basic development (git + system tooling):

**Kept (read-only):**
- `~/.gitconfig` (literal file)
- `~/.config/git/` (subpath — or `$XDG_CONFIG_HOME/git/` if set)

**Kept (read-write):**
- Project directory
- Runtime directory (`$XDG_RUNTIME_DIR/aide-<pid>/`)
- Temp directory (`$TMPDIR`)
- `~/.cache/` (build caches — go-build, pip, npm)
- `~/Library/Caches/` (macOS-native caches — Homebrew, Xcode)

**Kept (aide's own paths):**
- `~/.local/share/aide/` or `$XDG_DATA_HOME/aide/` (read-write —
  trust store, deny store)
- `~/.config/aide/` or `$XDG_CONFIG_HOME/aide/` (read-only — aide
  reads its own config; secrets are managed pre-sandbox)

**Kept (metadata/listing only):**
These allow tools to enumerate directory contents without reading
file data:
- Home directory listing (`file-read-data` on `$HOME` literal)
- Home metadata traversal (`file-read-metadata` on `$HOME` subpath)

**Removed from baseline — now require capabilities:**

| Previously in baseline | New capability |
|---|---|
| `~/.config/*` (all) | Split: `github` for `~/.config/gh`, `gcp` for `~/.config/gcloud`, etc. |
| `~/.ssh/*` | `ssh` |
| `~/.gnupg/*` | `gpg` (new) |
| `~/.cargo/*` | `rust` (new) |
| `~/.rustup/*` | `rust` (new) |
| `~/go/*` | `go` (new) |
| `~/.pyenv/*` | `python` (new) |
| `~/.rbenv/*` | `ruby` (new) |
| `~/.sdkman/*` | `java` (new) |
| `~/.gradle/*` | `java` (new) |
| `~/.m2/*` | `java` (new) |
| `~/Library/Keychains/*` | stays in `keychain` guard (always) |
| `~/Library/Preferences/*` | removed (not needed for dev) |
| Home dotfile regex `~/.[^/]+$` | removed — specific dotfiles via capabilities |

**Removed from baseline — build cache narrowing:**

The `~/.cache` read-write stays but `~/.gnupg` read-write moves to a
`gpg` capability. The `password-managers` guard protected
`~/.gnupg/private-keys-v1.d` — with `~/.gnupg` removed from baseline,
this guard becomes unnecessary unless the `gpg` capability is active.

### 2. New Builtin Capabilities (Language Runtimes)

| Capability | Unguard | Writable | Readable | EnvAllow |
|---|---|---|---|---|
| `go` | — | `~/go` | — | `GOPATH`, `GOROOT`, `GOBIN` |
| `rust` | — | `~/.cargo`, `~/.rustup` | — | `CARGO_HOME`, `RUSTUP_HOME` |
| `python` | — | `~/.pyenv` | — | `PYENV_ROOT`, `VIRTUAL_ENV` |
| `ruby` | — | `~/.rbenv` | — | `RBENV_ROOT`, `GEM_HOME` |
| `java` | — | `~/.sdkman`, `~/.gradle`, `~/.m2` | — | `JAVA_HOME`, `SDKMAN_DIR` |
| `github` | — | `~/.config/gh` | — | `GITHUB_TOKEN`, `GH_TOKEN` |
| `gpg` | — | `~/.gnupg` | — | `GNUPGHOME` |

Existing capabilities (`aws`, `gcp`, `docker`, `k8s`, `ssh`, `npm`,
`vault`, etc.) must have their `Unguard` fields cleared since the
corresponding guards are being removed. Dead references add confusion.

### 3. Guard Cleanup

Guards fall into three categories after this change:

**Keep (protect paths within writable baseline areas):**
- `project-secrets` — protects `.env` files and `.git/hooks` within
  project dir (which is writable). Still needed.
- `dev-credentials` — protects credential files within allowed dirs.
  Still needed for files within `~/.cache` or project dir.

**Redundant (paths no longer in baseline reads):**
- `cloud-aws` — `~/.aws` not in baseline
- `cloud-azure` — `~/.azure` not in baseline
- `cloud-oci` — `~/.oci` not in baseline
- `kubernetes` — `~/.kube` not in baseline
- `docker` — `~/.docker` not in baseline
- `terraform` — `~/.terraform.d` not in baseline
- `browsers` — `~/Library/Application Support/*` not in baseline
- `mounted-volumes` — `/Volumes` not in baseline

These guards still serve a purpose: when a capability enables access to
their path, the guard would deny specific sensitive files within it. But
capabilities already `Unguard` the corresponding guard. So these guards
only fire when the path is NOT accessible — meaning they deny what's
already denied. **They can be removed.**

**Action:** Remove redundant guards AND clear the `Unguard` fields
from their corresponding capabilities. `--with docker` no longer
means "unguard docker" — it means "grant writable access to
`~/.docker`". The guard concept is gone for these paths.

**Keep but re-evaluate:**
- `cloud-gcp` — if `gcp` capability grants `~/.config/gcloud` writable,
  the guard's deny of credential files within it serves no purpose (the
  capability unguards it). Remove.
- `cloud-digitalocean` — same reasoning. Remove.
- `github-cli` — same reasoning. Remove.
- `npm` — `~/.npmrc` no longer matched by dotfile regex. Guard denies
  what's already denied. Remove.
- `netrc` — `~/.netrc` same. Remove.
- `vault` — `~/.vault-token` same. Remove.
- `ssh-keys` — `~/.ssh` no longer in baseline. Guard denies what's
  already denied. Remove.
- `password-managers` — `~/.gnupg` no longer in baseline. Remove.

**Summary:** All "default" guards whose protected paths are no longer
in baseline reads become redundant and can be removed. Guards protecting
paths within writable baseline areas (project dir, caches) are retained.
Only "always" guards plus `project-secrets` and `dev-credentials`
remain.

### 4. Auto-Detection Expansion

Expand `internal/capability/detect.go` with marker file detection:

| Marker file(s) | Suggests capability |
|---|---|
| `go.mod`, `go.sum` | `go` |
| `Cargo.toml` | `rust` |
| `package.json` | `npm` (existing) |
| `pyproject.toml`, `requirements.txt`, `Pipfile`, `setup.py` | `python` |
| `Gemfile`, `*.gemspec` | `ruby` |
| `pom.xml`, `build.gradle`, `build.gradle.kts` | `java` |
| `Dockerfile`, `docker-compose.yml` | `docker` (existing) |
| `*.tf` | `terraform` (existing) |
| `Chart.yaml`, `helmfile.yaml` | `helm` (existing) |
| `k8s/`, `kubernetes/`, `manifests/` dirs | `k8s` (existing) |
| `.github/workflows/` | `github` |

Auto-detection **suggests only**, never auto-enables. Output:

```
Detected project capabilities: go, docker, k8s
Run: aide --with go,docker,k8s <agent>
Or save to .aide.yaml:
  capabilities:
    - go
    - docker
    - k8s
```

### 5. Trust Gate for .aide.yaml (direnv model)

`.aide.yaml` is untrusted by default. The trust mechanism uses
content-addressed hashing, identical to direnv.

**Trust check on launch:**

1. Compute `SHA-256(absolute_path + "\n" + file_contents)` → `fileHash`
2. Compute `SHA-256(absolute_path + "\n")` → `pathHash`
3. If `~/.local/share/aide/deny/<pathHash>` exists → **denied**, skip
   `.aide.yaml` silently
4. If `~/.local/share/aide/trust/<fileHash>` exists → **trusted**, apply
5. Otherwise → **untrusted**, show contents and prompt

**Trust commands:**

- `aide trust` — stores `fileHash` in
  `~/.local/share/aide/trust/<hash>`, removes any deny file
- `aide deny` — stores `pathHash` in
  `~/.local/share/aide/deny/<hash>`, removes any trust file.
  Deny is path-based (not content-based) because a denied project
  should stay denied even if the file changes — this prevents a
  malicious repo from cycling `.aide.yaml` contents to escape a deny.
- `aide untrust` — removes the trust hash without creating a deny
  entry, returning the file to the "untrusted/prompt" state
- `aide trust --path /prefix` — auto-approves `.aide.yaml` files
  under the given prefix on first encounter. Content changes still
  invalidate trust, but aide silently re-trusts instead of
  prompting. **Security note:** this is an explicit convenience
  trade-off — the user is declaring "I trust all repos under this
  prefix, including future `.aide.yaml` changes." Users should only
  use this for paths they fully control (e.g., `~/source`), not
  shared or cloned-from-others paths. Prefixes are stored in aide's
  user config and can be listed/removed via `aide trust --list` /
  `aide trust --remove`.

**Auto-re-trust:** When aide itself modifies `.aide.yaml` (via
`aide cap enable`, `aide sandbox allow`, etc.), it records the
pre-modification `fileHash` before writing. Auto-re-trust only
succeeds if the pre-modification hash matches the currently stored
trust hash. If someone else modified the file between the last trust
event and aide's modification, the user is re-prompted. This prevents
a partially-trusted file from being silently re-trusted after
external tampering.

**Atomicity:** Trust and deny files are written atomically (write to
temp file + rename) to prevent races between concurrent aide
processes.

**What is gated:** Everything in `.aide.yaml`:
- `capabilities`
- `disabled_capabilities`
- `sandbox` (all fields: writable, readable, denied, guards, unguard,
  network, allow_subprocess, clean_env)
- `mcp_servers`
- `agent`
- `secret`
- `env`
- `yolo`
- `preferences`

**Display on untrusted:**

```
$ aide
! .aide.yaml is not trusted

  Agent:        claude
  Capabilities: go, docker, k8s
  Sandbox:
    writable_extra: [~/.docker]
  Env:
    KUBECONFIG: ~/.kube/tails

  Run `aide trust` to approve this configuration.
  Run `aide deny` to permanently block it.
  Run `aide --ignore-project-config` to launch without it.
```

**Storage location:** `$XDG_DATA_HOME/aide/trust/` and
`$XDG_DATA_HOME/aide/deny/` (defaults to `~/.local/share/aide/`),
following XDG Base Directory spec.

### 6. Node Toolchain Special Case

The `node-toolchain` guard is an "always" guard that grants read access
to Node.js paths. With the new model, this should become a `node`
capability instead. However, many projects use Node.js tooling (prettier,
eslint, etc.) even if the project itself isn't Node.js.

**Decision:** Keep `node-toolchain` as an always guard for now. The
paths it allows are system-level Node installations, not user credential
directories. Revisit if this becomes a problem.

Similarly, `nix-toolchain` stays as-is — Nix store paths are
system-level, not user credentials.

### 7. Symlink Resolution

The current filesystem guard resolves home dotfile symlinks (for stow,
home-manager, etc.) and adds their targets. After removing the dotfile
regex, this broad symlink resolution becomes dead code.

**Decision:** Symlink resolution moves to per-path handling. The
baseline resolves symlinks only for `~/.gitconfig`. Each capability
resolves symlinks for its own paths at profile generation time (e.g.,
the `rust` capability resolves `~/.cargo` if it's a symlink).

## Migration

### For existing users

This is a breaking change. Users who relied on broad home reads without
`--with` flags will see sandbox blocks.

**Mitigation:**
1. On first run after upgrade, aide detects project capabilities and
   prints the suggestion banner prominently.
2. `aide doctor` checks for common breakage patterns and suggests
   capabilities.
3. Release notes document the change clearly.

### For .aide.yaml trust

All existing `.aide.yaml` files start as untrusted after upgrade. Users
must run `aide trust` once per project.

**Mitigation:**
- `aide trust --path ~/source` trusts all projects under a prefix,
  suitable for users who trust all their own repos.

## Non-Goals

- MCP server sandboxing (separate design, separate session)
- Custom guard creation by users
- Language-specific fine-grained permissions (e.g., "allow cargo but
  not cargo publish")
