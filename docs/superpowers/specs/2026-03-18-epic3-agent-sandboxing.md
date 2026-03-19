# Epic 3: Agent Sandboxing (P0) — Spec

**Date:** 2026-03-18
**Tasks:** 12, 13, 14, 15
**Design Decisions:** DD-14 (OS-native sandboxing, on by default), DD-15 (default sandbox policy), DD-17 (environment inheritance)
**External Dependency:** `github.com/landlock-lsm/go-landlock`

---

## Overview

Agent sandboxing restricts the filesystem, network, and process capabilities of
the launched coding agent. The sandbox is applied transparently: aide builds the
policy from config, generates OS-specific enforcement artifacts, and wraps the
agent's exec invocation so the agent runs inside the sandbox without knowing it.

Sandboxing is **on by default** (DD-14). A context with no `sandbox:` block gets
the default policy (DD-15). Setting `sandbox: false` in the context config
disables it entirely.

---

## Task 12: Sandbox Interface and Default Policy

### File: `internal/sandbox/sandbox.go`

This file defines the platform-agnostic interface and the default policy
constructor.

### Sandbox Interface

```go
package sandbox

import "os/exec"

// Sandbox applies a security policy to a command before execution.
// OS-specific implementations live in darwin.go and linux.go.
type Sandbox interface {
    // Apply modifies cmd in-place so that when cmd.Run() is called the
    // process executes inside the sandbox.  It may:
    //   - Rewrite cmd.Path and cmd.Args (e.g. prefix with sandbox-exec or bwrap)
    //   - Write temporary policy files to runtimeDir
    //   - Modify cmd.Env (for clean_env support)
    //
    // runtimeDir is the ephemeral $XDG_RUNTIME_DIR/aide-<pid>/ directory
    // that is cleaned on exit.  Policy files should be written here.
    //
    // Returns an error if the policy cannot be enforced on this OS/kernel.
    Apply(cmd *exec.Cmd, policy Policy, runtimeDir string) error
}
```

### Policy Struct

```go
// Policy describes the security boundary for an agent process.
type Policy struct {
    // Filesystem paths the agent may write to.
    Writable []string

    // Filesystem paths the agent may read (but not write).
    Readable []string

    // Filesystem paths the agent must not access at all.
    // Denied rules take precedence over Writable/Readable.
    Denied []string

    // Network mode: "outbound", "none", "unrestricted".
    Network NetworkMode

    // Whether the agent may spawn child processes.
    AllowSubprocess bool

    // When true the agent starts with only aide-injected env vars
    // (DD-17).  When false the agent inherits the full shell env.
    CleanEnv bool
}

type NetworkMode string

const (
    NetworkOutbound     NetworkMode = "outbound"
    NetworkNone         NetworkMode = "none"
    NetworkUnrestricted NetworkMode = "unrestricted"
)
```

### Default Policy Constructor

```go
// DefaultPolicy returns the sandbox policy applied when no sandbox: block
// exists in the context config (DD-15).
//
// Parameters:
//   projectRoot  — git root (or cwd if not a repo)
//   runtimeDir   — $XDG_RUNTIME_DIR/aide-<pid>/
//   homeDir      — user's home directory (~)
//   tempDir      — os.TempDir() result
func DefaultPolicy(projectRoot, runtimeDir, homeDir, tempDir string) Policy {
    return Policy{
        Writable: []string{
            projectRoot,
            runtimeDir,
            tempDir,
        },
        Readable: []string{
            projectRoot,
            "/usr/bin",
            "/usr/local/bin",
            "/bin",
            "/usr/lib",
            "/usr/share",
            filepath.Join(homeDir, ".gitconfig"),
            filepath.Join(homeDir, ".config/git"),
            filepath.Join(homeDir, ".ssh/known_hosts"),
        },
        Denied: []string{
            filepath.Join(homeDir, ".ssh/id_*"),
            filepath.Join(homeDir, ".aws/credentials"),
            filepath.Join(homeDir, ".azure"),
            filepath.Join(homeDir, ".config/gcloud"),
            filepath.Join(homeDir, ".config/aide/secrets"),
            filepath.Join(homeDir, "Library/Application Support/Google/Chrome"),
            filepath.Join(homeDir, ".mozilla"),
            filepath.Join(homeDir, "snap/chromium"),
        },
        Network:         NetworkOutbound,
        AllowSubprocess: true,
        CleanEnv:        false,
    }
}
```

### Platform Constructor

```go
// New returns the Sandbox implementation for the current OS.
// On macOS: DarwinSandbox.  On Linux: LandlockSandbox (with bwrap fallback).
// This function is defined in the build-tagged files (darwin.go / linux.go).
func New() Sandbox
```

The `New()` function lives in build-tagged files so the compiler only includes
the relevant implementation.

### File: `internal/sandbox/sandbox_unsupported.go`

Build tag: `//go:build !darwin && !linux`

Returns an error (or no-op) on unsupported platforms so the project compiles
everywhere.

```go
func New() Sandbox {
    return &unsupportedSandbox{}
}

type unsupportedSandbox struct{}

func (u *unsupportedSandbox) Apply(_ *exec.Cmd, _ Policy, _ string) error {
    // Log a warning that sandboxing is not available on this platform.
    // Do NOT return an error — the agent should still launch unsandboxed.
    return nil
}
```

### Tests — `internal/sandbox/sandbox_test.go`

| # | Test | Description |
|---|------|-------------|
| 1 | `TestDefaultPolicy_Paths` | Verify Writable contains projectRoot, runtimeDir, tempDir. Verify Readable contains gitconfig, ssh/known_hosts. Verify Denied contains ssh keys, aws creds. |
| 2 | `TestDefaultPolicy_NetworkMode` | Assert Network == NetworkOutbound. |
| 3 | `TestDefaultPolicy_Subprocess` | Assert AllowSubprocess == true. |
| 4 | `TestDefaultPolicy_CleanEnv` | Assert CleanEnv == false (DD-17 default). |
| 5 | `TestPolicy_DeniedPrecedence` | Unit-level: if a path appears in both Readable and Denied, Denied wins. (This is a contract test — enforcement is OS-specific, but the struct ordering is documented.) |

### Verification

```bash
go test ./internal/sandbox/ -run TestDefaultPolicy -v
go vet ./internal/sandbox/
```

---

## Task 13: macOS Sandbox — Seatbelt Profile Generation

### File: `internal/sandbox/darwin.go`

Build tag: `//go:build darwin`

### Design

macOS enforcement uses Apple's Seatbelt framework via the `sandbox-exec` CLI.
aide generates a `.sb` profile from the Policy struct, writes it to runtimeDir,
then rewrites `cmd.Path` and `cmd.Args` to invoke the agent through
`sandbox-exec -f <profile>`.

### DarwinSandbox

```go
type DarwinSandbox struct{}

func New() Sandbox {
    return &DarwinSandbox{}
}

func (d *DarwinSandbox) Apply(cmd *exec.Cmd, policy Policy, runtimeDir string) error {
    // 1. Generate Seatbelt profile string from policy
    profile, err := generateSeatbeltProfile(policy)
    if err != nil {
        return fmt.Errorf("generating seatbelt profile: %w", err)
    }

    // 2. Write profile to runtimeDir
    profilePath := filepath.Join(runtimeDir, "sandbox.sb")
    if err := os.WriteFile(profilePath, []byte(profile), 0600); err != nil {
        return fmt.Errorf("writing seatbelt profile: %w", err)
    }

    // 3. Rewrite cmd to wrap with sandbox-exec
    originalPath := cmd.Path
    originalArgs := cmd.Args // Args[0] is the program name

    cmd.Path = "/usr/bin/sandbox-exec"
    cmd.Args = append(
        []string{"sandbox-exec", "-f", profilePath},
        originalArgs...,
    )

    // 4. Handle clean_env (DD-17)
    if policy.CleanEnv {
        cmd.Env = filterEnv(cmd.Env)
    }

    return nil
}
```

### Seatbelt Profile Generation

The profile uses Seatbelt's TinyScheme-based format. The generated `.sb` file
follows this structure:

```scheme
(version 1)
(deny default)

;; --- Process ---
(allow process-exec)
(allow process-fork)          ;; only if AllowSubprocess == true

;; --- Network ---
(allow network-outbound)      ;; only if Network == "outbound" or "unrestricted"
(allow network-inbound)       ;; only if Network == "unrestricted"

;; --- Filesystem: denied (evaluated first by sandbox-exec) ---
(deny file-read* file-write*
    (subpath "/Users/x/.ssh/id_rsa")
    ...
)

;; --- Filesystem: writable ---
(allow file-read* file-write*
    (subpath "/path/to/project")
    (subpath "/var/folders/.../aide-12345")
    (subpath "/tmp")
)

;; --- Filesystem: readable ---
(allow file-read*
    (subpath "/usr/bin")
    (subpath "/usr/local/bin")
    (literal "/Users/x/.gitconfig")
    (subpath "/Users/x/.ssh/known_hosts")
)

;; --- System essentials (always allowed) ---
(allow file-read*
    (subpath "/usr/lib")
    (subpath "/System/Library")
    (subpath "/Library/Frameworks")
    (subpath "/private/var/db/dyld")
    (literal "/dev/null")
    (literal "/dev/urandom")
    (literal "/dev/random")
)
(allow sysctl-read)
(allow mach-lookup)
```

Implementation detail: `generateSeatbeltProfile(policy Policy) (string, error)`:

1. Start with `(version 1)` and `(deny default)`.
2. Always allow `process-exec`. Allow `process-fork` only when
   `AllowSubprocess` is true.
3. Map `Network` mode to Seatbelt network rules.
4. Emit deny rules for `Denied` paths. Use `(subpath ...)` for directories
   and `(literal ...)` for files. Expand glob patterns (e.g. `~/.ssh/id_*`)
   into multiple literals using `filepath.Glob` at generation time.
5. Emit allow read+write rules for `Writable` paths using `(subpath ...)`.
6. Emit allow read-only rules for `Readable` paths.
7. Append system essentials block (dyld cache, /dev/null, /dev/urandom, etc.)
   which are required for any process to start on macOS.

Use `text/template` or `strings.Builder` to assemble the profile. Prefer
`strings.Builder` for testability (easier to assert on fragments).

### Path Classification

Helper function to decide `(subpath ...)` vs `(literal ...)`:

```go
// seatbeltPath returns the Seatbelt path expression for a filesystem path.
// Directories use (subpath ...), files use (literal ...).
func seatbeltPath(p string) string {
    info, err := os.Stat(p)
    if err == nil && info.IsDir() {
        return fmt.Sprintf(`(subpath "%s")`, p)
    }
    return fmt.Sprintf(`(literal "%s")`, p)
}
```

For glob patterns in `Denied` (like `~/.ssh/id_*`), expand at generation time:

```go
func expandGlobs(patterns []string) []string {
    var result []string
    for _, p := range patterns {
        if strings.ContainsAny(p, "*?[") {
            matches, _ := filepath.Glob(p)
            result = append(result, matches...)
        } else {
            result = append(result, p)
        }
    }
    return result
}
```

### Tests — `internal/sandbox/darwin_test.go`

Build tag: `//go:build darwin`

| # | Test | Description |
|---|------|-------------|
| 1 | `TestGenerateSeatbeltProfile_DenyDefault` | Generated profile starts with `(version 1)` and `(deny default)`. |
| 2 | `TestGenerateSeatbeltProfile_WritablePaths` | Writable paths appear in `(allow file-read* file-write* ...)` block. |
| 3 | `TestGenerateSeatbeltProfile_ReadablePaths` | Readable paths appear in `(allow file-read* ...)` block but NOT in write block. |
| 4 | `TestGenerateSeatbeltProfile_DeniedPaths` | Denied paths appear in a `(deny file-read* file-write* ...)` block. |
| 5 | `TestGenerateSeatbeltProfile_NetworkOutbound` | Network=outbound produces `(allow network-outbound)` but no `(allow network-inbound)`. |
| 6 | `TestGenerateSeatbeltProfile_NetworkNone` | Network=none produces neither network-outbound nor network-inbound allows. |
| 7 | `TestGenerateSeatbeltProfile_NoSubprocess` | AllowSubprocess=false omits `(allow process-fork)`. |
| 8 | `TestGenerateSeatbeltProfile_SystemEssentials` | Profile always contains `/usr/lib`, `/System/Library`, `/dev/null`, `/dev/urandom`. |
| 9 | `TestGenerateSeatbeltProfile_GlobExpansion` | A Denied pattern `~/.ssh/id_*` expands to matching files (use a temp dir with test files). |
| 10 | `TestDarwinSandbox_Apply_RewritesCmd` | After Apply(), cmd.Path == `/usr/bin/sandbox-exec`, cmd.Args starts with `sandbox-exec -f <path>`, profile file exists in runtimeDir. |
| 11 | `TestDarwinSandbox_Apply_CleanEnv` | When CleanEnv=true, cmd.Env contains only aide-injected vars (PATH and explicitly set vars). |

### Integration Test — `internal/sandbox/darwin_integration_test.go`

Build tag: `//go:build darwin && integration`

| # | Test | Description |
|---|------|-------------|
| 1 | `TestDarwinSandbox_Integration_BlocksWrite` | Apply a policy that allows writing only to a temp dir. Spawn `touch /tmp/aide-test-blocked` (outside allowed path). Assert the process exits with a sandbox violation. |
| 2 | `TestDarwinSandbox_Integration_AllowsWrite` | Apply a policy that allows writing to a temp dir. Spawn `touch <tempdir>/allowed`. Assert success. |
| 3 | `TestDarwinSandbox_Integration_BlocksNetwork` | Apply a policy with Network=none. Spawn `curl -s https://example.com`. Assert failure. |

### Verification

```bash
go test ./internal/sandbox/ -run TestGenerateSeatbelt -v -tags darwin
go test ./internal/sandbox/ -run TestDarwinSandbox -v -tags darwin
# Integration (requires macOS):
go test ./internal/sandbox/ -run TestDarwinSandbox_Integration -v -tags 'darwin,integration'
```

---

## Task 14: Linux Landlock Sandboxing (+ bwrap Fallback)

### File: `internal/sandbox/linux.go`

Build tag: `//go:build linux`

### Design

Linux enforcement uses two mechanisms in priority order:

1. **Landlock** (preferred) — kernel 5.13+, via `go-landlock`. Self-sandboxing:
   the process restricts itself before exec'ing the agent. No external binary
   needed.
2. **bubblewrap (bwrap)** (fallback) — for older kernels. Uses Linux namespaces.
   Requires `bwrap` binary on PATH.

### LinuxSandbox

```go
type LinuxSandbox struct{}

func New() Sandbox {
    return &LinuxSandbox{}
}

func (l *LinuxSandbox) Apply(cmd *exec.Cmd, policy Policy, runtimeDir string) error {
    if landlockAvailable() {
        return l.applyLandlock(cmd, policy)
    }
    if bwrapPath, err := exec.LookPath("bwrap"); err == nil {
        return l.applyBwrap(cmd, policy, bwrapPath)
    }
    // Neither available — log warning, proceed unsandboxed.
    // Do NOT return error: the agent should still launch.
    log.Warn("sandboxing unavailable: kernel lacks Landlock and bwrap not on PATH")
    return nil
}
```

### Landlock Implementation

```go
import (
    "github.com/landlock-lsm/go-landlock/landlock"
)

func landlockAvailable() bool {
    // go-landlock exposes version detection.
    // Return true if kernel supports Landlock ABI >= v1.
    _, err := landlock.ABI()
    return err == nil
}

func (l *LinuxSandbox) applyLandlock(cmd *exec.Cmd, policy Policy) error {
    // Build Landlock rules using BestEffort() so that on kernels with
    // partial Landlock support, the strictest possible subset is enforced
    // rather than failing entirely.

    var rules []landlock.Rule

    // Writable paths: ReadWrite access
    for _, p := range policy.Writable {
        rules = append(rules, landlock.PathAccess(
            landlock.AccessFSReadWrite, p,
        ))
    }

    // Readable paths: ReadOnly access
    for _, p := range policy.Readable {
        rules = append(rules, landlock.PathAccess(
            landlock.AccessFSRead, p,
        ))
    }

    // Denied paths: Landlock doesn't have explicit deny.
    // Strategy: do NOT add denied paths to the allow lists.
    // Since Landlock is default-deny (only explicitly allowed paths
    // are accessible), omitting a path from rules effectively denies it.
    // Verify that no denied path is a subpath of an allowed path —
    // if it is, log a warning that Landlock cannot enforce sub-path
    // denial within an allowed parent (this is a Landlock limitation).

    // Network rules (Landlock ABI v4+, kernel 6.7+)
    if policy.Network == NetworkNone {
        // Restrict all network access by not granting network rights.
        // On older Landlock ABIs that don't support network, BestEffort()
        // will skip this restriction gracefully.
        rules = append(rules, landlock.NetAccess(0))
    }
    // NetworkOutbound and NetworkUnrestricted: grant network access.

    // Apply with BestEffort for graceful degradation on older kernels.
    err := landlock.V5.BestEffort().Restrict(rules...)
    if err != nil {
        return fmt.Errorf("landlock restrict: %w", err)
    }

    // Handle clean_env
    if policy.CleanEnv {
        cmd.Env = filterEnv(cmd.Env)
    }

    return nil
}
```

**Important Landlock note:** Landlock is self-sandboxing. Once
`landlock.Restrict()` is called, the **current process** (aide itself) is
restricted. This means Landlock restrictions must be applied in a **forked child
process** — not in the aide parent process. The implementation must either:

- Use `cmd.SysProcAttr` with a pre-exec hook that calls `landlock.Restrict()`
  in the child after fork but before exec. Go's `os/exec` does not directly
  support this, so the approach is:
- **Fork strategy:** aide execs a helper subprocess (itself, with a hidden
  `__sandbox-apply` subcommand) that applies Landlock and then execs the real
  agent. This is the pattern used by Anthropic's sandbox-runtime.

```go
// The aide binary re-execs itself with a special flag to apply Landlock
// in the child process before exec'ing the agent.
//
// Flow:
//   aide (parent) -> aide __sandbox-apply <policy-json> -- <agent> <args...>
//     child: parse policy, call landlock.Restrict(), exec agent

func (l *LinuxSandbox) applyLandlock(cmd *exec.Cmd, policy Policy, runtimeDir string) error {
    // 1. Serialize policy to JSON, write to runtimeDir
    policyPath := filepath.Join(runtimeDir, "landlock-policy.json")
    policyBytes, _ := json.Marshal(policy)
    os.WriteFile(policyPath, policyBytes, 0600)

    // 2. Rewrite cmd to re-exec aide with __sandbox-apply
    aideBin, _ := os.Executable()
    originalArgs := cmd.Args
    cmd.Path = aideBin
    cmd.Args = append(
        []string{"aide", "__sandbox-apply", policyPath, "--"},
        originalArgs...,
    )

    if policy.CleanEnv {
        cmd.Env = filterEnv(cmd.Env)
    }

    return nil
}
```

The `__sandbox-apply` handler (registered as a hidden cobra command or handled
in main before cobra):

```go
func runSandboxApply(policyPath string, agentCmd []string) error {
    // 1. Read and parse policy JSON
    // 2. Build landlock rules (same logic as above)
    // 3. Call landlock.V5.BestEffort().Restrict(rules...)
    // 4. syscall.Exec(agentCmd[0], agentCmd, os.Environ())
    //    (replaces the process with the agent)
}
```

### Bubblewrap Fallback

```go
func (l *LinuxSandbox) applyBwrap(cmd *exec.Cmd, policy Policy, bwrapPath string) error {
    // Build bwrap argument list from policy.

    var bwrapArgs []string

    // Writable paths: --bind <src> <src>
    for _, p := range policy.Writable {
        bwrapArgs = append(bwrapArgs, "--bind", p, p)
    }

    // Readable paths: --ro-bind <src> <src>
    for _, p := range policy.Readable {
        bwrapArgs = append(bwrapArgs, "--ro-bind", p, p)
    }

    // System essentials
    bwrapArgs = append(bwrapArgs,
        "--ro-bind", "/usr", "/usr",
        "--ro-bind", "/lib", "/lib",
        "--ro-bind", "/lib64", "/lib64",
        "--ro-bind", "/etc/resolv.conf", "/etc/resolv.conf",
        "--proc", "/proc",
        "--dev", "/dev",
        "--tmpfs", "/tmp",
    )

    // Denied paths: --tmpfs over the path (masks it with empty tmpfs)
    for _, p := range expandGlobs(policy.Denied) {
        bwrapArgs = append(bwrapArgs, "--tmpfs", p)
    }

    // Network isolation
    if policy.Network == NetworkNone {
        bwrapArgs = append(bwrapArgs, "--unshare-net")
    }

    // Process isolation
    if !policy.AllowSubprocess {
        // bwrap does not directly prevent subprocess spawning.
        // Log a warning that AllowSubprocess=false is not enforceable via bwrap.
    }

    // Clean env
    if policy.CleanEnv {
        bwrapArgs = append(bwrapArgs, "--clearenv")
        for _, e := range filterEnv(cmd.Env) {
            kv := strings.SplitN(e, "=", 2)
            bwrapArgs = append(bwrapArgs, "--setenv", kv[0], kv[1])
        }
    }

    // Rewrite cmd
    originalArgs := cmd.Args
    cmd.Path = bwrapPath
    cmd.Args = append(
        append([]string{"bwrap"}, bwrapArgs...),
        originalArgs...,
    )

    return nil
}
```

### Helper: filterEnv

Shared across darwin.go and linux.go (put in `sandbox.go` or a `helpers.go`):

```go
// filterEnv returns only the env vars that aide explicitly set,
// plus essential vars (PATH, HOME, USER, SHELL, TERM, LANG).
// Used when CleanEnv is true (DD-17).
func filterEnv(env []string) []string {
    essential := map[string]bool{
        "PATH": true, "HOME": true, "USER": true,
        "SHELL": true, "TERM": true, "LANG": true,
        "TMPDIR": true, "XDG_RUNTIME_DIR": true,
    }
    var filtered []string
    for _, e := range env {
        k := strings.SplitN(e, "=", 2)[0]
        if essential[k] {
            filtered = append(filtered, e)
        }
    }
    return filtered
}
```

### Tests — `internal/sandbox/linux_test.go`

Build tag: `//go:build linux`

| # | Test | Description |
|---|------|-------------|
| 1 | `TestLandlockAvailable` | On a kernel 5.13+ system, returns true. (Skip on older kernels.) |
| 2 | `TestApplyLandlock_RewritesCmd` | After Apply(), cmd.Path is the aide binary, cmd.Args contains `__sandbox-apply` and the policy path. |
| 3 | `TestApplyLandlock_PolicyJSON` | The written policy JSON file contains correct writable/readable/denied paths and network mode. |
| 4 | `TestApplyBwrap_RewritesCmd` | After Apply() with Landlock unavailable, cmd.Path is bwrap, cmd.Args contain `--bind` for writable and `--ro-bind` for readable paths. |
| 5 | `TestApplyBwrap_DeniedPaths` | Denied paths produce `--tmpfs` flags. |
| 6 | `TestApplyBwrap_NetworkNone` | Network=none produces `--unshare-net`. |
| 7 | `TestApplyBwrap_CleanEnv` | CleanEnv=true produces `--clearenv` and `--setenv` for essential vars. |
| 8 | `TestApplyBwrap_SystemEssentials` | bwrap args always include `--ro-bind /usr /usr`, `--proc /proc`, `--dev /dev`. |
| 9 | `TestLinuxSandbox_Fallback_NoSandbox` | When neither Landlock nor bwrap is available, Apply() returns nil (no error) and cmd is unmodified. |

### Integration Tests — `internal/sandbox/linux_integration_test.go`

Build tag: `//go:build linux && integration`

| # | Test | Description |
|---|------|-------------|
| 1 | `TestLandlock_Integration_BlocksWrite` | Apply Landlock policy allowing writes only to a temp dir. Fork+exec a helper that tries to write outside. Assert EACCES. |
| 2 | `TestBwrap_Integration_BlocksRead` | Apply bwrap with a denied path. Exec `cat <denied-path>`. Assert failure. |
| 3 | `TestBwrap_Integration_NetworkNone` | Apply bwrap with Network=none. Exec `curl`. Assert failure. |

### Verification

```bash
go test ./internal/sandbox/ -run TestLandlock -v -tags linux
go test ./internal/sandbox/ -run TestApplyBwrap -v -tags linux
# Integration (requires Linux kernel 5.13+ or bwrap):
go test ./internal/sandbox/ -run TestLandlock_Integration -v -tags 'linux,integration'
go test ./internal/sandbox/ -run TestBwrap_Integration -v -tags 'linux,integration'
```

---

## Task 15: Sandbox Policy Config Parsing

### File: `internal/sandbox/policy.go`

This file handles parsing the `sandbox:` block from context config YAML,
resolving template variables, and merging with defaults.

### Config Schema Addition

In `internal/config/schema.go`, add the sandbox config types:

```go
// SandboxConfig represents the sandbox: block in a context config.
// A nil pointer means "use defaults".  The bool variant (sandbox: false)
// is handled during YAML unmarshalling.
type SandboxConfig struct {
    // Disabled is true when the user writes `sandbox: false`.
    Disabled bool `yaml:"-"`

    Writable        []string `yaml:"writable,omitempty"`
    Readable        []string `yaml:"readable,omitempty"`
    Denied          []string `yaml:"denied,omitempty"`
    Network         string   `yaml:"network,omitempty"`
    AllowSubprocess *bool    `yaml:"allow_subprocess,omitempty"`
    CleanEnv        *bool    `yaml:"clean_env,omitempty"`
}

// UnmarshalYAML handles both `sandbox: false` (bool) and `sandbox: { ... }` (map).
func (s *SandboxConfig) UnmarshalYAML(value *yaml.Node) error {
    // If the node is a scalar boolean "false", set Disabled = true.
    if value.Kind == yaml.ScalarNode {
        var b bool
        if err := value.Decode(&b); err == nil && !b {
            s.Disabled = true
            return nil
        }
        return fmt.Errorf("sandbox: expected false or a mapping, got %q", value.Value)
    }
    // Otherwise decode as a struct (use an alias to avoid recursion).
    type alias SandboxConfig
    return value.Decode((*alias)(s))
}
```

### Template Variable Resolution

The `sandbox:` block supports the same `{{ .var }}` template syntax as env vars.
Available variables:

| Variable | Value |
|----------|-------|
| `{{ .project_root }}` | Git root directory (or cwd) |
| `{{ .runtime_dir }}` | `$XDG_RUNTIME_DIR/aide-<pid>/` |
| `{{ .home }}` | User's home directory |
| `{{ .config_dir }}` | `$XDG_CONFIG_HOME/aide/` |

Resolution reuses `internal/config/template.go`'s `ResolveTemplate` function.
Each path string in `writable`, `readable`, `denied` is resolved through the
template engine.

Additionally, tilde (`~`) at the start of a path is expanded to the home
directory.

```go
// ResolvePaths resolves template variables and ~ in a list of path strings.
func ResolvePaths(paths []string, vars map[string]string) ([]string, error) {
    var resolved []string
    for _, p := range paths {
        // 1. Resolve {{ .var }} templates
        r, err := ResolveTemplate(p, vars)
        if err != nil {
            return nil, fmt.Errorf("resolving path %q: %w", p, err)
        }
        // 2. Expand ~
        if strings.HasPrefix(r, "~/") {
            r = filepath.Join(vars["home"], r[2:])
        }
        resolved = append(resolved, r)
    }
    return resolved, nil
}
```

### Merging with Defaults

```go
// PolicyFromConfig builds a sandbox.Policy from a SandboxConfig and
// the default policy.  User-specified fields override defaults;
// unspecified fields use defaults.
//
// Merge rules:
//   - writable/readable/denied: if user specifies any entries, they REPLACE
//     the default list (not append).  This gives full control.
//   - network/allow_subprocess/clean_env: if user specifies, overrides default.
//   - sandbox: false → returns nil (no sandbox).
//   - sandbox: (absent / nil) → returns DefaultPolicy unchanged.
func PolicyFromConfig(
    cfg *SandboxConfig,
    projectRoot, runtimeDir, homeDir, tempDir string,
) (*Policy, error) {
    if cfg != nil && cfg.Disabled {
        return nil, nil // nil policy = no sandbox
    }

    defaults := DefaultPolicy(projectRoot, runtimeDir, homeDir, tempDir)

    if cfg == nil {
        return &defaults, nil
    }

    templateVars := map[string]string{
        "project_root": projectRoot,
        "runtime_dir":  runtimeDir,
        "home":         homeDir,
        "config_dir":   filepath.Join(homeDir, ".config", "aide"),
    }

    policy := defaults // copy

    if len(cfg.Writable) > 0 {
        w, err := ResolvePaths(cfg.Writable, templateVars)
        if err != nil {
            return nil, err
        }
        policy.Writable = w
    }

    if len(cfg.Readable) > 0 {
        r, err := ResolvePaths(cfg.Readable, templateVars)
        if err != nil {
            return nil, err
        }
        policy.Readable = r
    }

    if len(cfg.Denied) > 0 {
        d, err := ResolvePaths(cfg.Denied, templateVars)
        if err != nil {
            return nil, err
        }
        policy.Denied = d
    }

    if cfg.Network != "" {
        policy.Network = NetworkMode(cfg.Network)
    }

    if cfg.AllowSubprocess != nil {
        policy.AllowSubprocess = *cfg.AllowSubprocess
    }

    if cfg.CleanEnv != nil {
        policy.CleanEnv = *cfg.CleanEnv
    }

    return &policy, nil
}
```

### Validation

Add to `internal/config/validate.go` (or the existing validation path):

```go
func validateSandboxConfig(cfg *SandboxConfig) error {
    if cfg == nil || cfg.Disabled {
        return nil
    }
    validNetworkModes := map[string]bool{
        "outbound": true, "none": true, "unrestricted": true, "": true,
    }
    if !validNetworkModes[cfg.Network] {
        return fmt.Errorf(
            "sandbox.network: invalid value %q, must be one of: outbound, none, unrestricted",
            cfg.Network,
        )
    }
    return nil
}
```

### Tests — `internal/sandbox/policy_test.go`

| # | Test | Description |
|---|------|-------------|
| 1 | `TestSandboxConfig_UnmarshalYAML_False` | YAML `sandbox: false` produces SandboxConfig with Disabled=true. |
| 2 | `TestSandboxConfig_UnmarshalYAML_Map` | YAML `sandbox: { writable: [...], network: none }` parses correctly. |
| 3 | `TestSandboxConfig_UnmarshalYAML_Absent` | Context with no `sandbox:` key yields nil SandboxConfig. |
| 4 | `TestPolicyFromConfig_Nil_ReturnsDefaults` | Nil SandboxConfig returns DefaultPolicy. |
| 5 | `TestPolicyFromConfig_Disabled_ReturnsNil` | Disabled=true returns nil Policy (no sandbox). |
| 6 | `TestPolicyFromConfig_WritableOverride` | User-specified writable paths replace (not append to) defaults. |
| 7 | `TestPolicyFromConfig_ReadableOverride` | User-specified readable paths replace defaults. |
| 8 | `TestPolicyFromConfig_DeniedOverride` | User-specified denied paths replace defaults. |
| 9 | `TestPolicyFromConfig_NetworkOverride` | User-specified `network: none` overrides default outbound. |
| 10 | `TestPolicyFromConfig_AllowSubprocessOverride` | `allow_subprocess: false` overrides default true. |
| 11 | `TestPolicyFromConfig_CleanEnvOverride` | `clean_env: true` overrides default false. |
| 12 | `TestPolicyFromConfig_TemplateResolution` | `{{ .project_root }}` in writable paths resolves to actual project root. |
| 13 | `TestPolicyFromConfig_TildeExpansion` | `~/.gitconfig` expands to `/home/user/.gitconfig`. |
| 14 | `TestPolicyFromConfig_PartialOverride` | User specifies only `network: none`; all other fields use defaults. |
| 15 | `TestResolvePaths_InvalidTemplate` | A malformed template `{{ .nonexistent }}` returns an error. |
| 16 | `TestValidateSandboxConfig_InvalidNetwork` | `network: "foobar"` returns a validation error. |
| 17 | `TestValidateSandboxConfig_ValidModes` | All valid network modes ("outbound", "none", "unrestricted", "") pass. |

### Full YAML Round-Trip Test

```go
func TestSandboxConfig_FullRoundTrip(t *testing.T) {
    input := `
agent: claude
sandbox:
  writable:
    - "{{ .project_root }}"
    - "{{ .runtime_dir }}"
  readable:
    - "{{ .project_root }}"
    - "~/.gitconfig"
  denied:
    - "~/.ssh/id_*"
    - "~/.aws/credentials"
  network: outbound
  allow_subprocess: true
  clean_env: false
`
    // Parse YAML → SandboxConfig → PolicyFromConfig → verify all fields
}
```

### Verification

```bash
go test ./internal/sandbox/ -run TestSandboxConfig -v
go test ./internal/sandbox/ -run TestPolicyFromConfig -v
go test ./internal/sandbox/ -run TestResolvePaths -v
go test ./internal/sandbox/ -run TestValidateSandboxConfig -v
go test ./internal/config/ -run TestSandbox -v
```

---

## File Summary

| File | Task | Build Tag | Description |
|------|------|-----------|-------------|
| `internal/sandbox/sandbox.go` | 12 | (none) | `Sandbox` interface, `Policy` struct, `DefaultPolicy()`, `NetworkMode` constants, `filterEnv()` |
| `internal/sandbox/sandbox_unsupported.go` | 12 | `!darwin && !linux` | No-op `New()` for unsupported platforms |
| `internal/sandbox/darwin.go` | 13 | `darwin` | `DarwinSandbox`, Seatbelt `.sb` profile generation, `sandbox-exec` wrapping |
| `internal/sandbox/linux.go` | 14 | `linux` | `LinuxSandbox`, Landlock via go-landlock (re-exec pattern), bwrap fallback |
| `internal/sandbox/policy.go` | 15 | (none) | `SandboxConfig` YAML parsing, `PolicyFromConfig()`, `ResolvePaths()`, template + tilde expansion |
| `internal/config/schema.go` | 15 | (none) | `SandboxConfig` type added to context config schema |
| `internal/config/validate.go` | 15 | (none) | `validateSandboxConfig()` |
| `internal/sandbox/sandbox_test.go` | 12 | (none) | Tests for DefaultPolicy and Policy struct |
| `internal/sandbox/darwin_test.go` | 13 | `darwin` | Tests for Seatbelt profile generation |
| `internal/sandbox/darwin_integration_test.go` | 13 | `darwin,integration` | Live sandbox-exec tests |
| `internal/sandbox/linux_test.go` | 14 | `linux` | Tests for Landlock + bwrap |
| `internal/sandbox/linux_integration_test.go` | 14 | `linux,integration` | Live Landlock/bwrap tests |
| `internal/sandbox/policy_test.go` | 15 | (none) | Tests for config parsing, merging, template resolution |

---

## Integration with Launcher (Task 10)

The launcher (`internal/launcher/launcher.go`) calls the sandbox as the last
step before exec:

```go
// In the launch flow (step 7 from DESIGN.md):
sandbox := sandbox.New()
policy, err := sandbox.PolicyFromConfig(ctx.Sandbox, projectRoot, runtimeDir, homeDir, tempDir)
if err != nil {
    return fmt.Errorf("building sandbox policy: %w", err)
}
if policy != nil {
    if err := sandbox.Apply(cmd, *policy, runtimeDir); err != nil {
        return fmt.Errorf("applying sandbox: %w", err)
    }
}
// cmd.Run() or syscall.Exec(...)
```

---

## Dependency Setup

Add go-landlock to `go.mod`:

```bash
go get github.com/landlock-lsm/go-landlock@latest
```

This dependency is only imported in `linux.go` (build-tagged), so it does not
affect macOS builds.

---

## Implementation Order

1. **Task 12** first — defines the interface and types everything else depends on.
2. **Task 15** next — policy parsing can be tested without OS-specific code.
3. **Task 13** (macOS) and **Task 14** (Linux) in parallel — they are independent
   and only share the interface from Task 12.

```
T12 (interface + defaults) ──┬──> T15 (policy parsing)
                             ├──> T13 (macOS sandbox-exec)
                             └──> T14 (Linux Landlock + bwrap)
```
