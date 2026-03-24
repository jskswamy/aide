# Split-Read Sandbox Model with Discovery-Based Deny Guards

**Date:** 2026-03-24
**Status:** Draft
**Scope:** `pkg/seatbelt/guards/`, `internal/sandbox/`
**Depends on:** macOS sandbox fixes (same date, already implemented)

## Problem

The system-runtime guard whitelists specific system paths for read access.
Every new tool, SDK, or path not listed breaks inside the sandbox (whack-a-mole).
Meanwhile, `$HOME` is fully readable, exposing email databases, iMessage history,
other projects' source code and `.env` files, and app data no coding agent needs.

## Design Principles

1. **Don't block development.** aide exists to run coding agents. System paths
   (compilers, SDKs, frameworks) should always be readable.
2. **Least privilege for `$HOME`.** Only allow development paths the agent needs.
   Other projects, personal data, app databases are not accessible by default.
3. **Discover and deny.** Scan at profile generation time to find sensitive
   files (`.env`, shell history). Future-proof against new patterns.
4. **User control.** `readable_extra` opts into blocked paths, `writable_extra`
   grants write access. `unguard` removes specific guards.

## Core Change: Split-Read Model

### Outside `$HOME`: Broad system read

Allow read on all top-level system directories. These contain tools, compilers,
SDKs, frameworks — no user secrets.

```seatbelt
(allow file-read*
    (subpath "/usr")
    (subpath "/bin")
    (subpath "/sbin")
    (subpath "/opt")
    (subpath "/System")
    (subpath "/Library")
    (subpath "/nix")
    (subpath "/private")
    (subpath "/Applications")
    (subpath "/run")
    (subpath "/dev")
    (subpath "/tmp")
    (subpath "/var")
)
```

Also allow root-level metadata for traversal:

```seatbelt
(allow file-read-metadata (literal "/"))
(allow file-read-data (literal "/"))
```

**What this fixes:** Xcode shims (78 binaries in `/usr/bin`), `/Library/Developer`,
nix paths, and any future system tool — no more whack-a-mole for system paths.

**What this excludes:** `/Users/` (except through specific `$HOME` rules below)
and `/Volumes/` (explicitly denied, see below).

### Inside `$HOME`: Development paths only

Allow read on paths coding agents need for tool configuration and caches:

```seatbelt
;; XDG standard directories
(allow file-read* (subpath "~/.config"))
(allow file-read* (subpath "~/.cache"))
(allow file-read* (subpath "~/.local"))

;; Nix
(allow file-read* (subpath "~/.nix-profile"))
(allow file-read* (subpath "~/.nix-defexpr"))

;; Version managers and toolchains
(allow file-read* (subpath "~/.cargo"))
(allow file-read* (subpath "~/.rustup"))
(allow file-read* (subpath "~/go"))
(allow file-read* (subpath "~/.pyenv"))
(allow file-read* (subpath "~/.rbenv"))
(allow file-read* (subpath "~/.sdkman"))
(allow file-read* (subpath "~/.gradle"))
(allow file-read* (subpath "~/.m2"))

;; SSH (deny guard protects private keys within)
(allow file-read* (subpath "~/.ssh"))

;; Tool dotfiles (top-level files in $HOME)
(allow file-read* (regex #"^/Users/[^/]+/\.[^/]+$"))

;; macOS Library paths needed by dev tools
(allow file-read* (subpath "~/Library/Keychains"))
(allow file-read* (subpath "~/Library/Caches"))
(allow file-read* (subpath "~/Library/Preferences"))

;; Home directory metadata (for traversal)
(allow file-read-metadata
    (literal "~")
    (literal "~/Library")
)
```

The regex `^/Users/[^/]+/\.[^/]+$` matches dotfiles directly in `$HOME`
(`.gitconfig`, `.npmrc`, `.editorconfig`, etc.) but NOT subdirectories or
non-dot files.

### NOT accessible by default

These paths are simply not in any allow rule:

- `~/Documents/`, `~/Desktop/`, `~/Downloads/`
- `~/Pictures/`, `~/Music/`, `~/Movies/`
- `~/Library/Mail/`, `~/Library/Messages/`, `~/Library/Notes/`
- `~/Library/Calendars/`, `~/Library/Application Support/` (except guard paths)
- `~/source/other-project/` — other projects
- Any non-dot directory in `$HOME`

Users opt in with `readable_extra` or `writable_extra`.

## New Guard: `mounted-volumes` (default)

Denies read access to mounted volumes that may contain sensitive backups,
network shares, or encrypted volumes.

```seatbelt
(deny file-read-data (subpath "/Volumes"))
(deny file-write*    (subpath "/Volumes"))
```

User opt-out: `readable_extra: [/Volumes/MyDrive]`

## New Guard: `project-secrets` (default)

Discovery-based guard that protects secret files in the project directory.

### Discovery

At profile generation time, recursively scan `ctx.ProjectRoot` for:

- `.env` — dotenv files
- `.env.*` — environment-specific (`.env.local`, `.env.production`, etc.)
- `.envrc` — direnv files

**Directory skipping:** Always skip `.git/`, `node_modules/`, `vendor/`,
`__pycache__/`, `.venv/`, `venv/` regardless of `.gitignore`. For other
directories, respect `.gitignore` patterns to avoid scanning large trees.

**Implementation:** Use `github.com/sabhiram/go-gitignore` for gitignore
parsing, or shell out to `git ls-files --others --exclude-standard` for
correctness. The scan runs outside the sandbox where git is available.

**Clarification:** The scanner finds `.env*` files regardless of whether
they're gitignored. Gitignore is only used to skip directories for
performance (don't descend into `node_modules/`).

### Deny Rule Generation

For each discovered file, emit:

```seatbelt
(deny file-read-data (literal "/project/root/.env"))
(deny file-write*    (literal "/project/root/.env"))
```

### Deny `.git/hooks/` writes

The project root is read-write, but `.git/hooks/` is a persistence vector:
an agent can install hooks that fire outside the sandbox on the next
`git commit`, `git checkout`, etc.

```seatbelt
(deny file-write* (subpath "<project-root>/.git/hooks"))
```

User opt-out: `writable_extra: [.git/hooks]`

### User Opt-Out

Add to `readable_extra` or `writable_extra`:

```yaml
sandbox:
  writable_extra:
    - .env  # agent needs to modify .env
```

The guard checks `ctx.ExtraReadable` and `ctx.ExtraWritable` — if a
discovered path matches, skip the deny.

### Future Extensibility

Scan patterns can be extended later:
- `*.pem`, `*.key` — private keys in project
- `credentials.json` — service account keys
- `.secrets`, `.secret` — generic secret files

## New Guard: `shell-history` (default)

Denies shell history files that contain inline secrets.

### Paths Denied

```
~/.bash_history
~/.zsh_history
~/.local/share/fish/fish_history
~/.python_history
~/.node_repl_history
~/.irb_history
~/.psql_history
~/.mysql_history
~/.sqlite_history
~/.lesshst
```

Each path checked for existence before denying (skip if not found).
User opt-out: `readable_extra: [~/.zsh_history]`

## New Guard: `dev-credentials` (default)

Denies credential files within the allowed `~/.config/` and `~/.cargo/`
directories that contain auth tokens.

### Paths Denied

```
~/.config/gh/hosts.yml           — GitHub CLI OAuth tokens
~/.config/gh/config.yml          — GitHub CLI config
~/.cargo/credentials.toml        — crates.io publish token
~/.gradle/gradle.properties      — Maven/Artifactory tokens
~/.m2/settings.xml               — Maven repository credentials
~/.config/hub                    — Hub CLI GitHub token
~/.config/glab-cli/              — GitLab CLI token
~/.pypirc                        — PyPI upload credentials (dotfile in $HOME)
~/.gem/credentials               — RubyGems push token
```

Each path checked for existence before denying (skip if not found).
User opt-out: `readable_extra: [~/.config/gh]`

## Guard Promotions

### `netrc`: opt-in → default

`~/.netrc` contains plaintext credentials (username:password for hosts).
Now matched by the `$HOME` dotfile regex, so it's readable. Must be
default-denied.

### `kubernetes`: opt-in → default

`~/.kube/config` contains cluster credentials and tokens. `~/.kube/` is
not in the allowed dotdir list, but it could be added by a user. Promote
to default for defense in depth.

### `npm`: opt-in → default

`~/.npmrc` is explicitly readable (matched by dotfile regex) and commonly
contains npm/GitHub registry auth tokens. Must be default-denied.

### `docker`: opt-in → default

`~/.docker/config.json` contains registry auth tokens. `~/.docker/` is
matched by the dotfile regex. Must be default-denied.

### `github-cli`: opt-in → default

`~/.config/gh/` is inside the allowed `~/.config/` directory. Contains
GitHub OAuth tokens. Must be default-denied. (Also covered by the new
`dev-credentials` guard for defense in depth.)

## Guard Simplifications

With broad system reads and specific `$HOME` reads, read-only rules in
other guards become redundant:

### system-runtime

Remove all specific read path sections. Replace with the broad system
read rules listed above. Keep all non-read rules: process, temp dir
writes, device node writes, file ioctl, mach services, system socket,
IPC shared memory, launchd listener deny.

### nix-toolchain

Remove all read rules (covered by system reads + `$HOME` dotdir allows).
Keep: detection gate, daemon socket, nix user paths write.

### git-integration

**Remove entirely** — all rules were file-read* only, now covered by
`$HOME` dotfile allows and `~/.ssh/` allow.

### keychain

Remove read rules. Keep: write rules, Mach services, IPC shared memory.

### node-toolchain

**Unchanged** — all rules are `file-read* file-write*` combined.

### filesystem

Remove `readable` handling (`HomeDir` read-only no longer needed).
Keep: `writable` (ProjectRoot, RuntimeDir, TempDir, ExtraWritable),
`ExtraDenied`, and `ExtraReadable` (repurposed as deny opt-out).

### ssh-keys

Remove allow rules for safe files (`known_hosts`, `config`, `.pub`).
These are now readable via `~/.ssh/` subpath allow. Keep deny rules
for private keys.

## `ExtraReadable` Repurposing

`ExtraReadable` changes meaning:

| Before | After |
|--------|-------|
| Produces `(allow file-read*)` rules | Produces `(allow file-read*)` AND acts as deny opt-out |

The field now serves dual purpose: it adds allow rules for paths outside
the default set AND signals discovery guards to skip denies. A path in
`readable_extra` both becomes readable and is excluded from project-secrets
or other discovery denies.

---

## Out of Scope (Future Work)

The following threats were identified in the threat model but require
separate design efforts:

### 1. Environment Variable Scrubbing

**Threat:** Env vars like `AWS_SECRET_ACCESS_KEY`, `GITHUB_TOKEN`,
`ANTHROPIC_API_KEY` are inherited by the sandboxed process. The agent
runs `env` and exfiltrates everything. File-level deny guards don't help.

**Why separate:** This is a process-level mechanism, not a seatbelt rule.
Requires changes to `internal/launcher/` (env filtering before exec),
possibly a denylist of known dangerous env vars or an allowlist approach.

**Priority:** High — the single biggest remaining gap.

### 2. Network Egress Filtering

**Threat:** Any readable data can be exfiltrated over outbound network.
The sandbox allows `network: outbound` by default with no domain/IP
restrictions.

**Why separate:** Requires DNS-based allowlisting or proxy integration.
Significant infrastructure change beyond seatbelt rules.

**Priority:** High — fundamental limitation of any read-permissive sandbox.

### 3. Process-Exec Allowlisting

**Threat:** The agent can execute any binary on the system — `osascript`
(AppleScript GUI automation), `security` (keychain queries), `pbpaste`
(clipboard), `open` (launch apps outside sandbox). No path restriction on
`process-exec`.

**Why separate:** Restricting process-exec to specific paths is a major
refactor of the system-runtime guard. Needs careful analysis of what
binaries agents legitimately need.

**Priority:** Medium — mitigated by seatbelt inheritance (child processes
stay sandboxed) but `open` command may escape.

### 4. Keychain Hardening

**Threat:** The keychain guard allows full read-write + Mach services.
The agent can query keychain items via `security find-generic-password`.

**Why separate:** Restricting keychain access would break git credential
helpers and codesigning. Needs a tiered approach (read-only vs full access)
or per-operation Mach service filtering.

**Priority:** Medium — opt-in restriction for high-security environments.

### 5. `~/.config/` Discovery Guard

**Threat:** `~/.config/` is allowed broadly, but many CLI tools store
credentials there. The `dev-credentials` guard covers known paths, but
new tools (Copilot CLI, Cursor, etc.) could store secrets without guard
coverage.

**Why separate:** A discovery-based `~/.config/` guard (same pattern as
the old `macos-app-data` concept) would scan `~/.config/` and deny
unknown subdirectories. This is architecturally straightforward but
requires careful allowlisting of development tool configs.

**Priority:** Low — `dev-credentials` guard covers the known high-value
targets. Discovery can be added incrementally.

---

## Changes Summary

| File | Change |
|------|--------|
| **Core** | |
| `pkg/seatbelt/guards/guard_system_runtime.go` | Replace read paths with broad system reads |
| **New guards** | |
| `pkg/seatbelt/guards/guard_mounted_volumes.go` | New: deny `/Volumes/` |
| `pkg/seatbelt/guards/guard_project_secrets.go` | New: discovery .env/.envrc + deny .git/hooks writes |
| `pkg/seatbelt/guards/guard_shell_history.go` | New: deny shell history files |
| `pkg/seatbelt/guards/guard_dev_credentials.go` | New: deny credential files in allowed dirs |
| `pkg/seatbelt/guards/registry.go` | Register new guards, promote netrc/kubernetes/npm/docker/github-cli |
| **Guard simplifications** | |
| `pkg/seatbelt/guards/guard_nix_toolchain.go` | Remove read rules, keep socket + writes |
| `pkg/seatbelt/guards/guard_git_integration.go` | Empty or remove |
| `pkg/seatbelt/guards/guard_keychain.go` | Remove read rules, keep write + mach + IPC |
| `pkg/seatbelt/guards/guard_ssh_keys.go` | Remove allow rules, keep deny rules |
| `pkg/seatbelt/guards/guard_filesystem.go` | Remove readable, repurpose ExtraReadable |
| **Pipeline** | |
| `pkg/seatbelt/module.go` | Keep ExtraReadable (dual purpose) |
| `internal/sandbox/sandbox.go` | Keep ExtraReadable on Policy |
| **Tests** | |
| `pkg/seatbelt/guards/*_test.go` | Update for new guard outputs |
| `internal/sandbox/policy_contract_test.go` | Update contract tests |
| `internal/sandbox/darwin_test.go` | Update profile rendering tests |
| `internal/sandbox/integration_test.go` | Update ExtraReadable tests |

## Testing

**Unit tests:**
- system-runtime: assert broad system reads, no `$HOME` subpath
- mounted-volumes: assert deny on `/Volumes/`, verify readable_extra opt-out
- project-secrets: mock scan with .env files, verify deny rules, verify
  .git/hooks write deny, verify gitignore dir skip, verify opt-out
- shell-history: verify deny rules, verify skip on missing, verify opt-out
- dev-credentials: verify deny rules for each credential file
- Simplified guards: verify redundant reads removed, non-reads preserved
- Guard promotions: verify Type() returns "default" for promoted guards

**Contract tests:**
- writable_extra still produces file-write* rules
- readable_extra produces file-read* rules AND opts out of denies
- ExtraDenied still produces deny rules
- Cross-guard: node-toolchain writes not blocked by any new deny guard
- Cross-guard: nix writes not blocked by any new deny guard

**Integration tests (behind build tag):**
- stat /nix succeeds
- stat /Library/Developer succeeds
- go env GOROOT succeeds via nix symlinks
- .env file in project root is blocked
- .env file opted out via writable_extra is accessible
- ~/Documents/ is not readable (not in allow list)
- Other project directory is not readable
