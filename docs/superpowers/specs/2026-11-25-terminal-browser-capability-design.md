# Built-in `terminal-browser` Capability with Auto-Detection

Status: Approved (Issue #36)
Date: 2026-11-25

## Intent

Running `terminal-browser` CLI commands (`terminal-browser ls`, `terminal-browser action -- ...`, `terminal-browser open`) inside an `aide` sandbox fails with `EPERM`:

```text
terminal-browser: EPERM: operation not permitted, open '~/.local/share/terminal-browser-58a67c5b/install'
```

Even read-only commands call `ensureDataDir()` on startup (writing `<DATA_DIR>/install`) and connect to Unix sockets under `<RUNTIME_HOME>/<APP_DIR_NAME>/`, where `<APP_DIR_NAME>` is `terminal-browser-<8-hex>` for release installs or `terminal-browser-dev-<8-hex>` for dev checkouts. Because the 8-character hex suffix depends on the install prefix (Apple Silicon Homebrew, Intel Homebrew, Linux, dev checkouts), a static path in a built-in capability cannot cover all hosts.

This design adds a built-in `terminal-browser` capability to `aide` that:
1. Auto-enables **only** when `terminal-browser` on-disk state is detected on the host (never enabled on machines without it, and always removable via `--without terminal-browser` or `disabled_capabilities`).
2. Dynamically resolves `terminal-browser`'s concrete XDG, config, interop, and macOS Unix-socket rules at launch and inspection time (`aide cap show/check/audit`, `aide status`, `aide sandbox show`), across both macOS Seatbelt and Linux Landlock.

## Goals, Non-Goals, and Anti-Goals

- **Goals**:
  - `aide cap list` includes the built-in `terminal-browser` capability.
  - Auto-enabled via `capability.AutoInclude` only when at least one existing `^terminal-browser-(dev-)?[0-9a-f]{8}$` directory exists on the host under XDG data, state, or runtime roots.
  - `terminal-browser ls`, `terminal-browser action -- ...`, and `terminal-browser open` work inside the sandbox across Homebrew upgrades, different install prefixes, dev checkouts, `clean_env: true`, and restrictive network modes (`allow_ports`, `network: none`), including alongside `rex` / `tmux` / Kitty / Ghostty.
  - Works on both macOS (Seatbelt) and Linux (Landlock) using the same Go-side concrete directory resolution.
- **Non-Goals**:
  - Cold-start support before `terminal-browser` has ever run once on the host (creating `terminal-browser-<hash>` dirs from scratch inside the sandbox). Users run `terminal-browser` at least once on the host, which is always true when driving an existing pane.
  - Launching or managing `terminal-browser`'s Electron GUI app (or its `favicons/` cache) from inside the sandbox.
  - Granting `TERMINAL_BROWSER_TERMINAL_SOCKET` or `network-bind` in v1 (deferred unless live testing proves them necessary).
  - Adding new `.aide.yaml` config fields.
- **Anti-Goals**:
  - Coupling `aide` to `terminal-browser`'s internal `stableIdentity` + `sha256[:8]` hashing algorithm.
  - Granting broad parent directories (`~/.local/share`, `~/.local/state`, `~/.config`), trusting unchecked env-var path overrides, or interpolating unescaped paths into Seatbelt rules.
  - Enabling `terminal-browser` on hosts where it has not been set up.

## Approaches Considered

1. **Raw `-*` glob in `builtin.go` (`~/.local/share/terminal-browser-*`)**:
   - *Tradeoffs*: Zero extra Go code, but **broken on Linux** because only Seatbelt expands globs (`seatbelt.ExpandGlobs`); Landlock passes literal strings to the kernel. Also matches arbitrary `terminal-browser-*` names and ignores custom `XDG_*` / `TERMINAL_BROWSER_*` env vars.
2. **Compute `stableIdentity` + `sha256[:8]` from `PATH` binary (+ cold-start rules)**:
   - *Tradeoffs*: Fast (~200 ns for SHA-256 of the path string, < 0.5 ms with `LookPath` + `EvalSymlinks`), but couples `aide` to upstream's hash formula, misses secondary dev checkouts, and non-existent paths are dropped by `validateAndFilterPaths` (`internal/sandbox/policy.go:232-246`) unless extra macOS-only Seatbelt `Allow` rules are synthesized.
3. **Go-side concrete directory enumeration + validated XDG/env resolution (Selected)**:
   - *Tradeoffs*: Matches `^terminal-browser-(dev-)?[0-9a-f]{8}$` subdirectories under the resolved XDG roots in Go (matching how the existing `skillshare` capability resolves dynamic paths). Works identically on macOS and Linux, survives Homebrew upgrades and upstream hash changes, supports both release and dev checkouts, validates all env overrides against protected dirs, and keeps the implementation lean. Requires `terminal-browser` to have run at least once on the host.

## Design

### Terminology & Existing Primitives

- **Capability markers (`Markers`)**: Project-directory file/glob rules (e.g., `go.mod` for `go`) that suggest capabilities for a repo. Host-level tools (`ccstatusline`, `bivouac`, `ponytail`, `skillshare`, and `terminal-browser`) have no project `Markers` and instead use `AutoInclude`.
- **Harness**: An AI coding agent driver supported by `aide` (e.g., Claude Code, Codex, Pi), each with its own default and profile config directories under `$HOME`.
- **`modules.SensitiveHomeDirs()`**: The list of sensitive directories under `$HOME` (such as `~/.ssh`, `~/.aws`, `~/.gnupg`, etc.) in `pkg/seatbelt/modules` that dynamic capability resolvers must never expose.
- **`homepath.XDG(envVal, home, fallbackRel)`**: Helper in `internal/homepath` that returns `envVal` if it is a non-empty absolute path (per the XDG Base Directory spec), otherwise `filepath.Join(home, fallbackRel)`.
- **`clean_env` / `EnvAllow`**: When `clean_env: true` is active, `aide` strips host environment variables outside a 9-key essential set (`PATH`, `HOME`, `USER`, `SHELL`, `TERM`, `LANG`, `TMPDIR`, `XDG_RUNTIME_DIR`, `XDG_CONFIG_HOME`) plus agent-module keys. `Capability.EnvAllow` currently exempts credential-like names from `envscrub.ScrubCredentials`; Section 5 extends `clean_env` filtering to also preserve non-credential `EnvAllow` keys.
- **Seatbelt `Allow` entries**: Strings in `Capability.Allow` omit the outer `(allow ...)` wrapper because `pkg/seatbelt/guards/guard_filesystem.go` wraps each entry as `(allow %s)`.

### 1. Built-in Capability (`internal/capability/builtin.go`)

Add `"terminal-browser"` to `builtins`:
- `Name`: `"terminal-browser"`
- `Description`: `"terminal-browser CLI data, state/runtime sockets, config (read-only), and interop paths (auto-included when terminal-browser has been run on the host)."`
- `EnvAllow`:
  - `TERMINAL_BROWSER_CONFIG_DIR`
  - `TERMINAL_BROWSER_INTEROP_DIR`
  - `TERMINAL_BROWSER_INTEROP_TARGET`
  - `XDG_DATA_HOME`
  - `XDG_STATE_HOME`
  - *(Note: `XDG_CONFIG_HOME` and `XDG_RUNTIME_DIR` are already preserved in the baseline `clean_env` essential set; `XDG_CACHE_HOME` is omitted because the CLI does not require the Electron favicons cache.)*
- Static `Readable`, `Writable`, `Allow`, and `Markers` fields are left empty; dynamic paths and macOS Unix-socket `Allow` rules are populated at resolution time by Section 4.

### 2. Host Detection & Auto-Include (`internal/capability/terminal_browser.go` & `autoinclude.go`)

Add a pure helper in `internal/capability/terminal_browser.go` (imports only stdlib + `internal/homepath`, avoiding any import cycle with `internal/sandbox`):
- `TerminalBrowserHashedDirs(homeDir string, getenv func(string) string) (dataDirs, stateDirs, runtimeDirs []string)`:
  - Resolves `DATA_HOME` (`XDG_DATA_HOME` → `.local/share`), `STATE_HOME` (`XDG_STATE_HOME` → `.local/state`), and `RUNTIME_HOME` (`XDG_RUNTIME_DIR` if absolute and non-empty, else `STATE_HOME`).
  - Scans direct children of each root for names matching `^terminal-browser-(dev-)?[0-9a-f]{8}$`.
  - Uses `os.Stat` (not `DirEntry.IsDir()`) on each regex-matched entry so symlinked directories are included while regular files and broken symlinks are skipped.
- `TerminalBrowserInstalled(homeDir string, getenv func(string) string) bool`:
  - Returns `true` **only** when at least one hashed directory exists in `dataDirs`, `stateDirs`, or `runtimeDirs`.
  - Does **not** call `exec.LookPath`: this matches all 4 existing auto-included capabilities (`ccstatusline`, `bivouac`, `ponytail`, `skillshare`), keeps `AutoInclude` pure with respect to `homeDir`/`getenv` (no host-`PATH` leakage into unit tests), and guarantees `"terminal-browser"` is never activated as a zero-grant no-op before first host run.
- In `internal/capability/autoinclude.go`:
  - Add `autoInclude(capNames, excluded, "terminal-browser", TerminalBrowserInstalled(homeDir, getenv))`.
  - Honors `--without terminal-browser`, `disabled_capabilities`, and deduplication. Explicit `--with terminal-browser` remains available on any machine.

### 3. Shared Path Guard & Dynamic Resolver (`internal/pathguard/` & `internal/sandbox/terminal_browser.go`)

To eliminate duplication of security-critical path validation without circular imports:
- **Extract shared `internal/pathguard` package** (imports `internal/config`, `internal/fsutil`, `internal/harness`, `internal/homepath`, `pkg/seatbelt/modules`; imported by both `internal/provision/skillshare` and `internal/sandbox`):
  - Moves `grantChecker` and `protectedDirs` out of `internal/provision/skillshare/grants.go` into an exported `pathguard.Checker`.
  - Enforces:
    1. Reject non-absolute paths and paths containing `{{` (template injection).
    2. Resolve symlinks via `filepath.EvalSymlinks` (missing paths and symlink loops are skipped).
    3. Require `info.IsDir()` on the resolved target for directory grants.
    4. Reject `/`, resolved `$HOME`, and any path overlapping (ancestor or descendant of) `modules.SensitiveHomeDirs()`, harness default/profile config dirs, or `aide`'s own config dir.
    5. For **all env-derived writable grants** (`TERMINAL_BROWSER_INTEROP_DIR` and hashed dirs discovered under `XDG_DATA_HOME`, `XDG_STATE_HOME`, or `XDG_RUNTIME_DIR`), additionally require **strict** child containment (`fsutil.IsUnderDir(p, root) && p != root`) under one of: resolved `$HOME`, `/run/user/<uid>`, or the current user's resolved `os.TempDir()` tree — and explicitly reject system roots (`/etc`, `/var/run`, `/private/var/run`, `/usr`, `/System`, `/Library`, `/tmp`, `/private/tmp`). (This prevents granting `/run/user/<uid>` itself or another user's `/private/var/folders/...` dir, and avoids a self-referential `$XDG_RUNTIME_DIR` bypass.)
    6. Return both resolved and literal paths when they differ so Seatbelt and Landlock match symlinked locations.
  - Provide `pathguard.SafeSeatbeltPath(p string) bool` rejecting `"`, `\`, `(`, `)`, `\n`, `\r`, `\x00`, and any non-printable ASCII control character (`< 0x20` or `0x7f`) before any path is interpolated into a Seatbelt `Allow` rule.

- **Dynamic `terminal-browser` resolver (`internal/sandbox/terminal_browser.go`)**:
  - Uses `capability.TerminalBrowserHashedDirs(home, getenv)` and `pathguard.NewChecker(cfg, home, getenv)` to collect:
    - **Writable**:
      - Validated hashed dirs under `DATA_HOME`, `STATE_HOME`, and `RUNTIME_HOME`.
      - Interop dirs: if `TERMINAL_BROWSER_INTEROP_DIR` is set and absolute, that validated dir; otherwise `~/.local/state/terminal-browser-interop` and `~/.local/share/terminal-browser-interop` if they exist. *(Note: `terminal-browser`'s `interopRoot` hardcodes `~/.local/{state,share}/terminal-browser-interop` and ignores `XDG_STATE_HOME`/`XDG_DATA_HOME` unless `TERMINAL_BROWSER_INTEROP_DIR` is set, so `aide` matches `terminal-browser`'s exact fallback.)*
    - **Readable (read-only)**:
      - Config dir: `TERMINAL_BROWSER_CONFIG_DIR` if set and absolute, else `filepath.Join(CONFIG_HOME, "terminal-browser")` if it exists (validated).
    - **macOS Seatbelt `Allow` rules** (`goos == "darwin"` only):
      - For each validated socket-bearing directory (hashed dirs under `RUNTIME_HOME`, plus the validated `TERMINAL_BROWSER_INTEROP_DIR` or `~/.local/state/terminal-browser-interop`), verify `pathguard.SafeSeatbeltPath(resolvedDir)` and emit:
        `network-outbound (remote unix-socket (subpath "<resolvedDir>"))`
      - On Linux Landlock, connecting to a filesystem-backed Unix domain socket is governed by filesystem read/write access on the socket's directory (already granted via `Writable`), so no extra socket rule is needed. (Under Linux `bwrap` with `network: none` / `--unshare-net`, filesystem-backed sockets work via `--bind-try`, while abstract Unix sockets cannot cross the network namespace.)

### 4. Unified Dynamic-Path Wiring (`internal/sandbox/capabilities.go` & `cmd/aide/`)

Replace `sandbox.AddSkillsharePaths(caps, cfg)` directly across all 5 internal callers with `sandbox.AddDynamicPaths(caps, cfg)`:
- `internal/sandbox/capabilities.go` (`ResolveCapabilitiesWithVariants`, which feeds `ToSandboxOverrides()` and `launcher.Launch`)
- `cmd/aide/cap.go` (`capShowCmd`, `capCheckCmd`, `capAuditCmd`)
- `cmd/aide/status.go` (`statusCmd`)

Inside `AddDynamicPaths`:
- Iterates `caps` and only runs a dynamic resolver when `slices.Contains(rc.Sources, "skillshare")` or `slices.Contains(rc.Sources, "terminal-browser")`. This guarantees zero filesystem scanning when a capability is absent/disabled and ensures capabilities using `extends` or `combines` inherit dynamic `Readable`, `Writable`, and `Allow` grants.
- Note on display commands: `aide cap show`, `aide cap check`, `aide cap audit`, and `aide status` all display the dynamic `Readable`/`Writable` paths; Seatbelt `Allow` rules are displayed by `aide cap show` and `aide sandbox show`.

### 5. Preserve `Capability.EnvAllow` Under `clean_env` (`internal/launcher/` & `internal/sandbox/`)

Because `launcher.filterEssentialEnv` (`internal/launcher/launcher.go:871-885`) and `sandbox.filterEnv` (`internal/sandbox/sandbox.go:555-573`) currently keep only the hardcoded 9-key essential set (+ `agentEnvKeys`), custom `XDG_DATA_HOME`, `XDG_STATE_HOME`, and `TERMINAL_BROWSER_*` vars would be stripped under `clean_env: true` even though `aide` granted their paths.

- Propagate `capOverrides.EnvAllow` (already filtered against `never_allow_env` by `Set.ToSandboxOverrides()`) into both `launcher.filterEssentialEnv` and `sandbox.Policy.EnvAllow` → `sandbox.filterEnv`.
- **Both** `launcher.filterEssentialEnv` and `sandbox.filterEnv` enforce `!envscrub.Credentials().Matches(k)` on `EnvAllow` keys, so credential-named variables from other active capabilities (e.g., `AWS_SECRET_ACCESS_KEY`, `GITHUB_TOKEN`) are never leaked by `clean_env: true` — even under `--no-sandbox --clean-env` — while non-credential capability vars (`TERMINAL_BROWSER_*`, `XDG_DATA_HOME`, `XDG_STATE_HOME`) survive cleanly.

### 6. Documentation Sync (`AGENTS.md` requirement)

- Update `docs/capabilities.md` to document the `terminal-browser` built-in capability, its on-disk auto-detection, its granted paths/env vars, the host first-run prerequisite, and the Linux `bwrap --unshare-net` abstract-socket caveat.

## Security Analysis

- **Config is read-only**: `$CONFIG_HOME/terminal-browser` (`settings.json`, `shortcuts.json`) is granted `Readable`, preventing a sandboxed agent from tampering with host browser settings or shortcuts.
- **SBPL injection prevention**: Every path interpolated into a Seatbelt `Allow` rule is validated by `pathguard.SafeSeatbeltPath` (rejecting `"`, `\`, `(`, `)`, newlines, and control characters) before emission.
- **Env-override & protected-dir validation**: All candidate paths are symlink-resolved, checked with `info.IsDir()`, rejected if overlapping sensitive/harness/aide/system dirs, and writable env overrides are constrained to `$HOME` or per-user runtime/temp roots.
- **Strict basename matching**: Only directories matching `^terminal-browser-(dev-)?[0-9a-f]{8}$` (plus the fixed interop dirs) are granted — sibling directories in `~/.local/share` and `~/.local/state` remain denied.
- **Scoped Unix sockets**: Seatbelt `network-outbound` rules are scoped to `(remote unix-socket (subpath "<resolvedDir>"))` for the specific resolved runtime/interop directories, granting no TCP/UDP egress or unrelated host sockets.
- **Opt-out preserved**: `--without terminal-browser` and `disabled_capabilities: [terminal-browser]` always prevent auto-inclusion.

## Testing & Verification Strategy

1. **Unit tests**:
   - `internal/capability/builtin_test.go`: verify `"terminal-browser"` is registered with expected `EnvAllow` and no static `Markers`.
   - `internal/capability/autoinclude_test.go`: verify auto-include enables `"terminal-browser"` when a matching hashed dir (including a symlinked dir) exists under data/state/runtime, stays disabled when none exists (even if a binary were on `PATH`), and honors `excluded`.
   - `internal/pathguard/` tests: verify protected-dir rejection (`~/.ssh`, `$HOME`, `/`, system roots like `/private/var/run`, non-dir files, symlink loops), writable env-override boundary enforcement, and `SafeSeatbeltPath` rejection of `"`, `\`, `(`, `)`, `\n`, and control chars; verify `skillshare` tests continue passing unchanged.
   - `internal/sandbox/terminal_browser_test.go` & `capabilities_test.go`: verify hashed dir enumeration (release + dev + symlinked), non-matching sibling rejection, custom `XDG_*` and `TERMINAL_BROWSER_*` overrides, default interop fallback (`~/.local/{state,share}/terminal-browser-interop`), macOS Unix-socket `Allow` emission (including for `TERMINAL_BROWSER_INTEROP_DIR`), Linux omission of Seatbelt `Allow` rules, `extends`/`combines` inheritance via `rc.Sources`, and `AddDynamicPaths` covering both `skillshare` and `terminal-browser`.
   - `internal/sandbox/` & `internal/launcher/` `clean_env` tests: verify `capOverrides.EnvAllow` keys (`TERMINAL_BROWSER_*`, `XDG_DATA_HOME`, `XDG_STATE_HOME`) survive `clean_env: true` while `never_allow_env` and credential-named keys remain stripped.
2. **Live verification on macOS (`aide sandbox test`)**:
   - Run `terminal-browser ls`, `terminal-browser action -- ...`, and `terminal-browser open` under the generated sandbox policy (including under `network: none` / `allow_ports`, `clean_env: true`, and alongside `rex` / `tmux` / Kitty / Ghostty).
   - Confirm whether `CONFIG_DIR` is strictly read-only at runtime and whether `network-bind` or `TERMINAL_BROWSER_TERMINAL_SOCKET` is needed; if live testing reveals a required write or socket, surface it as a material spec update.
   - Verify sibling directories under `~/.local/share` and `~/.local/state` and writes to `~/.config/terminal-browser` remain denied.

---

## Architecture Grounding Evidence

- **Code revision**: `a5b80d0` (`main`, clean working tree at grounding time).
- **Index status**: `codebase-memory-mcp` project `Users-subramk-source-github.com-jskswamy-aide` ready (`last_scan_at: 2026-11-25T18:14:30Z`, 0 parse errors); all affected symbols and callers verified directly in source.
- **Analyzed scope & source references**:
  - `internal/capability/builtin.go` (built-in capability definitions)
  - `internal/capability/autoinclude.go` (`AutoInclude` — 5 production caller functions / 6 call sites: `launcher.Launch`, `launcher.PreviewSessionEnv`, `cmd/aide.resolveEffectiveCapabilities` [2x], `cmd/aide.whichCmd`, `cmd/aide.statusCmd`)
  - `internal/sandbox/capabilities.go` (`ResolveCapabilitiesWithVariants`, `AddSkillsharePaths` — 5 call sites across `internal/sandbox/` and `cmd/aide/cap.go`, `cmd/aide/status.go`)
  - `internal/provision/skillshare/grants.go` (`Installed`, `ReadableDirs`, `grantChecker`, `protectedDirs`)
  - `internal/launcher/launcher.go:516-525,871-885` (`filterEssentialEnv`, `launchScrubRegistry`)
  - `internal/sandbox/policy.go:225-246` (`isGlobPattern`, `validateAndFilterPaths`), `internal/sandbox/sandbox.go:555-573` (`filterEnv`)
  - `internal/sandbox/linux.go` (Landlock `ExtraWritable` handling — no glob expansion; `bwrap --unshare-net` at lines 943–946)
  - `pkg/seatbelt/path.go:47-65` (`ExpandGlobs`), `pkg/seatbelt/guards/guard_filesystem.go:119-124` (verbatim `(allow %s)` emission), `pkg/seatbelt/guards/guard_network.go`, `pkg/seatbelt/guards/registry.go:18-27` (`NetworkGuard` before `FilesystemGuard`), `pkg/seatbelt/guards/filesystem_test.go:502` (`~/.cache` baseline grant)
  - Host `terminal-browser` v0.15.3 (`/opt/homebrew/Caskroom/terminal-browser/0.15.3/terminal-browser/cli/dist/main.js` lines 2455–2506, 22597–22620)
- **Coverage gaps**:
  - Live runtime probe of `terminal-browser ls` / `action` / `open` under `aide sandbox test` (deferred to implementation verification to confirm read-only `CONFIG_DIR` and whether `network-bind` or `TERMINAL_BROWSER_TERMINAL_SOCKET` is touched).
  - Linux Landlock execution (code-verified via `internal/sandbox/linux.go`; live Linux test requires a Linux host).

## Review Record

- **Review mode**: Full — user selected EGM review + Ponytail review
- **Scope/rationale**: Built-in `terminal-browser` capability, `AutoInclude` on-disk detection, shared `internal/pathguard` extraction, unified `sandbox.AddDynamicPaths`, and `clean_env` `EnvAllow` passthrough
- **Reviewed candidate & provenance**: `docs/superpowers/specs/2026-11-25-terminal-browser-capability-design.md` against `a5b80d0`
- **Completed review outcomes**:
  - `ponytail:ponytail-review` (proposed-design review, 2026-11-25): 3 Must fix (Blocker), 4 Should fix (Major), 1 Nice to have (Nit) — **all 8 resolved** in the specification.
  - `egm-review` (`egm-comprehension`, `egm-design`, `egm-codebase-details`, `egm-zoom-out`, 2026-11-25): Pass 1 found 2 Blockers and 7 Majors — all resolved; targeted re-review (`egm-design`, `egm-codebase-details`) passed import graphs, `SafeSeatbeltPath`, on-disk `AutoInclude`, `os.Stat`, and interop mapping, and flagged 2 remaining Majors (`filterEssentialEnv` missing `!envscrub.Credentials().Matches(k)` under `--no-sandbox --clean-env`; writable env-override strict containment under `$HOME` / `/run/user/<uid>` / `os.TempDir()`), both now resolved in this revision.
- **Final user approval**: Approved by user on 2026-11-26 ("approved go ahead and get it done in a worktree").
