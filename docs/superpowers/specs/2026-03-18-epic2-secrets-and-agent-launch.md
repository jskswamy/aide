# Epic 2: Secrets & Agent Launch (P0) -- Tasks 6-11

**Date:** 2026-03-18
**Status:** Draft
**Dependencies:** Epic 1 (Tasks 1-5) must be complete. External: `github.com/getsops/sops/v3/decrypt`
**Design Decisions Referenced:** DD-2, DD-4, DD-7, DD-10, DD-11, DD-13, DD-16, DD-17

---

## Overview

This epic implements the secret decryption pipeline, template resolution, ephemeral runtime directory management, the agent launcher that ties everything together, and zero-config passthrough for users who do not need configuration. After this epic, a user can run `aide` and have it decrypt secrets, resolve templates, launch the correct agent with the correct environment, and clean up on exit.

### Dependency Graph (Tasks 6-11)

```
T6 (age discovery) --> T7 (sops decrypt) --> T8 (template resolution)
                                                      |
                                                      v
                                              T9 (runtime dir)
                                                      |
                                                      v
                                              T10 (launcher)
                                                      |
                                                      v
                                              T11 (zero-config)

T5 (context matcher, from Epic 1) ----+----> T10
                                      +----> T11
```

---

## Task 6: Age Key Discovery

**File:** `internal/secrets/age.go`
**Test file:** `internal/secrets/age_test.go`

### Purpose

Locate the age private key (or YubiKey identity) that sops will use for decryption. This is the first step in the decryption pipeline. The discovery order follows the sops ecosystem conventions with YubiKey prioritized for hardware-bound security (DD-13).

### Discovery Order

The function tries each source in order and returns the first successful result:

1. **YubiKey** -- detect `age-plugin-yubikey` on PATH. If present, return an age plugin identity string. The YubiKey holds the private key in hardware; the identity file on disk contains only the public key and plugin reference.
2. **`SOPS_AGE_KEY` environment variable** -- if set, the value is the raw age secret key string (e.g., `AGE-SECRET-KEY-1...`). Used in CI/Docker where writing key files is undesirable.
3. **`SOPS_AGE_KEY_FILE` environment variable** -- if set, points to a file containing one or more age secret keys (one per line).
4. **Default key path:** `$XDG_CONFIG_HOME/sops/age/keys.txt` -- the standard location that `age-keygen` and sops both use by default. Resolve via `github.com/adrg/xdg`.

### Interface

```go
package secrets

// AgeKeySource describes where an age key was found.
type AgeKeySource int

const (
    SourceYubiKey     AgeKeySource = iota
    SourceEnvKey                        // SOPS_AGE_KEY
    SourceEnvKeyFile                    // SOPS_AGE_KEY_FILE
    SourceDefaultFile                   // XDG default path
)

// AgeIdentity holds a discovered age identity for sops decryption.
type AgeIdentity struct {
    Source AgeKeySource
    // KeyData contains the raw key material for SourceEnvKey,
    // or the file path for SourceEnvKeyFile / SourceDefaultFile.
    // For SourceYubiKey this is empty (plugin handles it).
    KeyData string
}

// DiscoverAgeKey searches for an age identity in priority order.
// Returns the first identity found, or an error with guidance
// if no identity is available.
func DiscoverAgeKey() (*AgeIdentity, error)
```

### Implementation Notes

- For YubiKey detection, use `exec.LookPath("age-plugin-yubikey")`. Do NOT attempt to communicate with the YubiKey at discovery time -- sops handles that during decryption.
- For `SOPS_AGE_KEY`, validate the value starts with `AGE-SECRET-KEY-` prefix before accepting it.
- For file-based sources, verify the file exists and is readable. Do NOT read the file contents into memory at discovery time -- just confirm the path is valid.
- For the default path, use `xdg.ConfigHome` from `github.com/adrg/xdg` to resolve `sops/age/keys.txt`.
- The error message when no key is found must be actionable:
  ```
  No age identity found. aide needs an age key to decrypt secrets.

  Options:
    1. Plug in a YubiKey with age-plugin-yubikey installed
    2. Set SOPS_AGE_KEY env var (for CI/Docker)
    3. Set SOPS_AGE_KEY_FILE to point to your key file
    4. Run: age-keygen -o ~/.config/sops/age/keys.txt

  Run 'aide setup' for guided configuration.
  ```

### Environment Setup for Sops

After discovering the key source, the launcher (Task 10) must set the appropriate environment variable before calling sops decrypt, because the sops library reads these variables internally:

| Source | Action before decrypt |
|---|---|
| `SourceYubiKey` | No env setup needed. sops + age-plugin-yubikey handle it via the identity file. |
| `SourceEnvKey` | `SOPS_AGE_KEY` is already set. No action needed. |
| `SourceEnvKeyFile` | Set `SOPS_AGE_KEY_FILE` if not already set. |
| `SourceDefaultFile` | Set `SOPS_AGE_KEY_FILE` to the resolved path. |

### TDD Tests

| # | Test | Description |
|---|------|-------------|
| 1 | `TestDiscoverAgeKey_YubiKeyOnPath` | Place a fake `age-plugin-yubikey` binary in a temp PATH dir. Verify `SourceYubiKey` is returned. |
| 2 | `TestDiscoverAgeKey_EnvKey` | Set `SOPS_AGE_KEY=AGE-SECRET-KEY-1FAKE...`. Verify `SourceEnvKey` returned with key data. |
| 3 | `TestDiscoverAgeKey_EnvKeyInvalid` | Set `SOPS_AGE_KEY=not-a-valid-key`. Verify it is skipped (does not return an error, falls through to next source). |
| 4 | `TestDiscoverAgeKey_EnvKeyFile` | Create a temp key file, set `SOPS_AGE_KEY_FILE` to it. Verify `SourceEnvKeyFile` returned. |
| 5 | `TestDiscoverAgeKey_EnvKeyFileMissing` | Set `SOPS_AGE_KEY_FILE=/nonexistent`. Verify it is skipped. |
| 6 | `TestDiscoverAgeKey_DefaultPath` | Create a key file at the expected XDG path (use `t.Setenv` to override `XDG_CONFIG_HOME`). Verify `SourceDefaultFile`. |
| 7 | `TestDiscoverAgeKey_PriorityOrder` | Set both `SOPS_AGE_KEY` and a default key file. Put fake yubikey on PATH. Verify YubiKey wins. Remove yubikey from PATH, verify env key wins. |
| 8 | `TestDiscoverAgeKey_NoneFound` | Clear all env vars, empty PATH, no default file. Verify error message contains setup guidance. |

### Verification

```bash
go test ./internal/secrets/ -run TestDiscoverAgeKey -v
```

---

## Task 7: Sops Decryption via Library

**File:** `internal/secrets/sops.go`
**Test file:** `internal/secrets/sops_test.go`

### Purpose

Decrypt a sops-encrypted YAML file into a `map[string]string` using the sops Go library. This avoids a runtime dependency on the `sops` CLI binary (DD-4). The decrypted data lives only in process memory and is never written to disk.

### Interface

```go
package secrets

import "fmt"

// DecryptSecretsFile decrypts a sops-encrypted YAML file and returns
// the key-value pairs as a flat string map.
//
// The filePath is resolved relative to $XDG_CONFIG_HOME/aide/secrets/
// unless it is an absolute path. The AgeIdentity is used to set up
// the environment for sops decryption.
func DecryptSecretsFile(filePath string, identity *AgeIdentity) (map[string]string, error)
```

### Implementation Notes

- Import `github.com/getsops/sops/v3/decrypt`.
- Call `decrypt.File(absPath, "yaml")`. This returns `[]byte` of the decrypted YAML.
- Unmarshal the decrypted YAML into `map[string]interface{}` first, then flatten to `map[string]string`. All values are converted to strings via `fmt.Sprintf("%v", val)`. Only top-level keys are supported in the initial implementation (no nested maps).
- Before calling `decrypt.File`, set the appropriate environment variable based on the `AgeIdentity.Source` (see Task 6 table). Restore the original env after decryption using `defer`.
- The `filePath` resolution logic:
  - If absolute path, use as-is.
  - If relative, join with `$XDG_CONFIG_HOME/aide/secrets/`.
- Error wrapping must include the file path and a hint about key configuration:
  ```
  failed to decrypt secrets/work: <sops error>.
  Is your YubiKey plugged in? Check 'aide setup' for key configuration.
  ```

### Handling Non-String Values

The secrets file should contain only string values (API keys, tokens). If a value is not a string (e.g., a nested map or a list), return an error:

```
secrets file %s contains non-string value for key %q. Only flat key-value pairs are supported.
```

### TDD Tests

| # | Test | Description |
|---|------|-------------|
| 1 | `TestDecryptSecretsFile_Success` | Create a sops-encrypted test fixture (checked into `testdata/`). Decrypt and verify key-value pairs match expected. Use `SOPS_AGE_KEY` env with a test key. |
| 2 | `TestDecryptSecretsFile_RelativePath` | Pass `personal` (relative). Verify it resolves to `$XDG_CONFIG_HOME/aide/secrets/personal`. |
| 3 | `TestDecryptSecretsFile_AbsolutePath` | Pass an absolute path. Verify it is used as-is. |
| 4 | `TestDecryptSecretsFile_FileNotFound` | Pass a nonexistent path. Verify clear error. |
| 5 | `TestDecryptSecretsFile_WrongKey` | Encrypt with one age key, try to decrypt with a different one. Verify error includes guidance. |
| 6 | `TestDecryptSecretsFile_NonStringValue` | Create fixture with nested YAML. Verify error about non-string values. |
| 7 | `TestDecryptSecretsFile_EmptyFile` | Decrypt an empty (but valid) sops file. Verify empty map returned, no error. |

### Test Fixture Setup

Generate test fixtures with a dedicated test age key (committed to the repo -- this is a TEST key with no real secrets):

```bash
# Generate a test-only key pair (commit both to repo under testdata/)
age-keygen -o internal/secrets/testdata/test-key.txt 2>internal/secrets/testdata/test-key.pub

# Create plaintext test secrets
cat > /tmp/test-secrets.yaml <<EOF
api_key: sk-test-12345
token: tok-test-67890
EOF

# Encrypt with the test public key
sops encrypt --age $(cat internal/secrets/testdata/test-key.pub) \
  /tmp/test-secrets.yaml > internal/secrets/testdata/test.enc.yaml

rm /tmp/test-secrets.yaml
```

### Verification

```bash
go test ./internal/secrets/ -run TestDecryptSecretsFile -v
```

---

## Task 8: Template Resolution for Env Vars

**File:** `internal/config/template.go`
**Test file:** `internal/config/template_test.go`

### Purpose

Resolve `{{ .secrets.xxx }}` and other template expressions in config values using Go's `text/template` (DD-2). This converts the env var map from config (with template strings) into a fully resolved env var map (with actual values).

### Template Data

The template context provides three data sources:

```go
// TemplateData holds the data available inside {{ }} expressions.
type TemplateData struct {
    Secrets     map[string]string // from sops decryption (Task 7)
    ProjectRoot string            // git root or cwd
    RuntimeDir  string            // ephemeral dir (Task 9)
}
```

Usage in templates:

| Expression | Resolves to |
|---|---|
| `{{ .secrets.anthropic_api_key }}` | Value of `anthropic_api_key` from decrypted secrets file |
| `{{ .project_root }}` | Absolute path to the git root of the current project |
| `{{ .runtime_dir }}` | Path to `$XDG_RUNTIME_DIR/aide-<pid>/` |

### Interface

```go
package config

// ResolveTemplates processes a map of env var definitions, resolving
// any {{ }} template expressions against the provided data.
// Values without template syntax are passed through unchanged (DD-11).
// Returns a new map with all templates resolved.
func ResolveTemplates(env map[string]string, data *TemplateData) (map[string]string, error)

// IsTemplate returns true if the string contains {{ }} template syntax.
func IsTemplate(s string) bool
```

### Implementation Notes

- Use `text/template` from the standard library. Create a new template per value string.
- Set `Option("missingkey=error")` so that referencing a nonexistent secrets key produces a clear error rather than an empty string.
- Values without `{{ }}` pass through unchanged. Use `IsTemplate()` (simple `strings.Contains` check for `{{`) to short-circuit non-template values. This supports DD-11 (optional secrets / gradual adoption).
- The `.secrets` field in the template maps to `TemplateData.Secrets`. Access pattern: `{{ .secrets.KEY }}` -- this uses Go template's map indexing on the `Secrets` field (works natively when Secrets is `map[string]string`). Actually, since `text/template` accesses struct fields via dot notation, the template data struct must expose `Secrets` as a field. To make `{{ .secrets.KEY }}` work with a lowercase `s`, use a template data map:

```go
// Build the template data as a map for case-matching with templates
data := map[string]interface{}{
    "secrets":      secretsMap,      // map[string]string
    "project_root": projectRoot,     // string
    "runtime_dir":  runtimeDir,      // string
}
```

- Error messages must identify the specific env var and the missing key:
  ```
  template error in env var "AWS_PROFILE": key "aws_profile" not found in secrets.
  Available keys: anthropic_api_key, aws_region
  ```

### TDD Tests

| # | Test | Description |
|---|------|-------------|
| 1 | `TestResolveTemplates_SecretRef` | Input: `{"API_KEY": "{{ .secrets.api_key }}"}`, secrets: `{"api_key": "sk-123"}`. Verify output: `{"API_KEY": "sk-123"}`. |
| 2 | `TestResolveTemplates_ProjectRoot` | Input: `{"ROOT": "{{ .project_root }}"}`. Verify output contains the project root path. |
| 3 | `TestResolveTemplates_RuntimeDir` | Input: `{"DIR": "{{ .runtime_dir }}"}`. Verify output contains the runtime dir path. |
| 4 | `TestResolveTemplates_Literal` | Input: `{"FLAG": "1"}`. Verify `"1"` passes through unchanged (no template processing). |
| 5 | `TestResolveTemplates_Mixed` | Input has both template and literal values. Verify each resolves correctly. |
| 6 | `TestResolveTemplates_MissingKey` | Reference `{{ .secrets.nonexistent }}`. Verify error names the key and lists available keys. |
| 7 | `TestResolveTemplates_InvalidSyntax` | Input: `"{{ .secrets.foo"` (unclosed). Verify parse error with the env var name in the message. |
| 8 | `TestResolveTemplates_EmptySecrets` | Template references secrets but secrets map is nil. Verify clear error (not a nil pointer panic). |
| 9 | `TestResolveTemplates_NoSecretsNeeded` | All values are literals, secrets map is nil. Verify success (no error even though secrets is nil). |
| 10 | `TestResolveTemplates_ComplexTemplate` | `"--project {{ .project_root }} --key {{ .secrets.key }}"`. Verify both are resolved in a single string. |

### Verification

```bash
go test ./internal/config/ -run TestResolveTemplates -v
```

---

## Task 9: Ephemeral Runtime Dir Management

**File:** `internal/launcher/runtime.go`
**Test file:** `internal/launcher/runtime_test.go`

### Purpose

Create and manage an ephemeral directory at `$XDG_RUNTIME_DIR/aide-<pid>/` for generated MCP configs and other temporary files that contain decrypted secrets. This directory must be cleaned up on exit to ensure secrets never persist on disk (DD-10).

### Interface

```go
package launcher

// RuntimeDir manages an ephemeral directory for aide's runtime files.
type RuntimeDir struct {
    Path string
    pid  int
}

// NewRuntimeDir creates a new runtime directory at
// $XDG_RUNTIME_DIR/aide-<pid>/ with mode 0700.
// Falls back to os.TempDir() if XDG_RUNTIME_DIR is not set.
func NewRuntimeDir() (*RuntimeDir, error)

// Cleanup removes the runtime directory and all its contents.
// Safe to call multiple times.
func (r *RuntimeDir) Cleanup() error

// RegisterSignalHandlers sets up signal handlers for SIGTERM, SIGINT,
// SIGQUIT, and SIGHUP that trigger Cleanup before exit.
// Returns a cancel function to deregister the handlers.
func (r *RuntimeDir) RegisterSignalHandlers() context.CancelFunc

// CleanStale removes any leftover aide-* directories in
// $XDG_RUNTIME_DIR that belong to processes that no longer exist.
// Called on startup to handle SIGKILL edge cases.
func CleanStale() error
```

### Implementation Notes

- **Directory path:** `$XDG_RUNTIME_DIR/aide-<pid>/` where pid is `os.Getpid()`. On Linux, `XDG_RUNTIME_DIR` is typically `/run/user/<uid>/` (tmpfs -- cleared on reboot). On macOS, it is often unset; fall back to `os.TempDir()` (usually `/tmp/`).
- **Permissions:** `os.MkdirAll(path, 0700)`. Verify after creation that the directory mode is exactly 0700. If the directory already exists (e.g., stale from a crashed previous run with the same PID -- rare but possible after PID wraparound), remove it first and recreate.
- **Signal handlers:** Use `signal.NotifyContext` or `signal.Notify` on a channel for `syscall.SIGTERM`, `syscall.SIGINT`, `syscall.SIGQUIT`, `syscall.SIGHUP`. The signal handler goroutine calls `Cleanup()` then `os.Exit(1)`. This goroutine must be started by `RegisterSignalHandlers` and stoppable via the returned cancel function.
- **Stale cleanup (`CleanStale`):** On startup, list directories matching `aide-*` in `$XDG_RUNTIME_DIR`. For each, extract the PID from the directory name. Check if that PID is still running (`os.FindProcess` + `p.Signal(syscall.Signal(0))`). If the process is gone, remove the directory. Log a message at debug level for each cleaned directory.
- **Cleanup idempotency:** `Cleanup()` must be safe to call from both the `defer` path and the signal handler. Use `sync.Once` internally.

### Directory Lifecycle

```
aide starts
  |
  +--> CleanStale()                    // remove orphaned aide-* dirs
  |
  +--> NewRuntimeDir()                 // create aide-<pid>/ with 0700
  |
  +--> RegisterSignalHandlers()        // SIGTERM/INT/QUIT/HUP -> Cleanup
  |
  +--> [write generated MCP configs, aggregator configs into dir]
  |
  +--> exec agent (dir path passed via env or config)
  |
  +--> on exit:
         defer Cleanup()               // normal exit
         signal handler -> Cleanup()   // signal exit
         SIGKILL -> next launch CleanStale() handles it
```

### TDD Tests

| # | Test | Description |
|---|------|-------------|
| 1 | `TestNewRuntimeDir_Creates` | Call `NewRuntimeDir()`. Verify directory exists with mode 0700. |
| 2 | `TestNewRuntimeDir_UsesXDGRuntimeDir` | Set `XDG_RUNTIME_DIR` to a temp dir. Verify the runtime dir is created inside it. |
| 3 | `TestNewRuntimeDir_FallsBackToTempDir` | Unset `XDG_RUNTIME_DIR`. Verify the runtime dir is created inside `os.TempDir()`. |
| 4 | `TestRuntimeDir_Cleanup` | Create dir, write a file into it, call `Cleanup()`. Verify directory and contents are gone. |
| 5 | `TestRuntimeDir_CleanupIdempotent` | Call `Cleanup()` twice. Verify no error on second call. |
| 6 | `TestRuntimeDir_CleanupOnSignal` | Start a goroutine, register signal handlers, send `SIGINT` to self. Verify directory is cleaned up. (Use a subprocess test pattern to avoid killing the test process.) |
| 7 | `TestCleanStale_RemovesOrphanedDirs` | Create `aide-99999/` (a PID that does not exist). Call `CleanStale()`. Verify it is removed. |
| 8 | `TestCleanStale_PreservesLiveDirs` | Create `aide-<current-pid>/`. Call `CleanStale()`. Verify it is NOT removed. |
| 9 | `TestNewRuntimeDir_ReplacesExisting` | Create `aide-<current-pid>/` manually, put a file in it. Call `NewRuntimeDir()`. Verify old contents are gone, new dir exists. |

### Verification

```bash
go test ./internal/launcher/ -run "TestNewRuntimeDir|TestRuntimeDir|TestCleanStale" -v
```

---

## Task 10: Agent Launcher

**File:** `internal/launcher/launcher.go`
**Test file:** `internal/launcher/launcher_test.go`

### Purpose

Orchestrate the full launch flow: resolve context, decrypt secrets, resolve templates, create runtime dir, build environment, exec the agent binary. This is the "main loop" that ties Tasks 6-9 together with the context resolution from Epic 1.

### Launch Flow

The launcher executes the 9-step launch flow from the Security Model section of DESIGN.md:

```
Step 1: Read config.yaml
Step 2: Resolve context (git remote + path matching)
Step 3: Decrypt secrets in memory (Tasks 6 + 7)
Step 4: Create $XDG_RUNTIME_DIR/aide-<pid>/ (Task 9)
Step 5: Generate MCP/aggregator config into runtime dir (Epic 4 -- stubbed here)
Step 6: Build env vars via template resolution (Task 8)
Step 7: Apply sandbox policy (Epic 3 -- stubbed here)
Step 8: Exec agent with env + MCP config path
Step 9: On exit: rm -rf runtime dir
```

### Interface

```go
package launcher

import "github.com/jskswamy/aide/internal/config"

// LaunchOptions holds runtime overrides from CLI flags.
type LaunchOptions struct {
    AgentOverride   string   // --agent flag
    ContextOverride string   // --context flag
    CleanEnv        bool     // --clean-env flag (DD-17)
    Verbose         bool     // -v flag
    AgentArgs       []string // remaining args forwarded to agent (DD-16)
}

// Launch executes the full agent launch flow.
// It does not return on success (syscall.Exec replaces the process).
// Returns an error only if the launch fails.
func Launch(cfg *config.Config, opts *LaunchOptions) error
```

### Implementation Notes

#### Step 3: Decrypt Secrets

- If the resolved context has no `secret`, skip decryption. The secrets map is nil -- template resolution will still work for literal values (DD-11).
- If `secret` is set, call `DiscoverAgeKey()` then `DecryptSecretsFile()`.

#### Step 5: MCP Config Generation (Stub)

- In this epic, MCP generation is not yet implemented (Epic 4). Create a placeholder:
  ```go
  // TODO(epic4): Generate MCP/aggregator config into runtimeDir.Path
  ```

#### Step 6: Build Environment

- Start with the current process environment (`os.Environ()`) unless `CleanEnv` is true (DD-17).
- If `CleanEnv`, start with a minimal set: `PATH`, `HOME`, `USER`, `SHELL`, `TERM`, `LANG`, `TMPDIR`.
- Apply the resolved context env vars (output of `ResolveTemplates`) on top, overriding any existing values.
- Set `AIDE_RUNTIME_DIR` to the runtime dir path (so the agent and MCP servers can reference it).
- Set `AIDE_CONTEXT` to the resolved context name (for debugging).

#### Step 7: Sandbox (Stub)

- In this epic, sandboxing is not yet implemented (Epic 3). Create a placeholder:
  ```go
  // TODO(epic3): Apply sandbox policy before exec.
  ```

#### Step 8: Exec Agent

- Resolve the agent binary path: look up `agents[name].binary` from config. If not set, the agent name IS the binary name. Use `exec.LookPath` to find the absolute path.
- Build the argv: `[binary] + AgentArgs` (DD-16 -- forward all remaining args).
- Use `syscall.Exec(binaryPath, argv, envSlice)` to replace the aide process with the agent. This is critical: aide does NOT use `os/exec.Command` with `Start()/Wait()`. It uses `syscall.Exec` so the agent becomes PID 1 of the process tree and signal handling is clean.
- Before `syscall.Exec`, ensure the runtime dir cleanup is set up via defer + signal handlers. Since `syscall.Exec` replaces the process, the defer will NOT run after exec succeeds. The signal handlers registered in the aide process are also replaced. This means: the runtime dir cleanup for the normal case relies on the agent exiting and the OS cleaning tmpfs on reboot, plus `CleanStale()` on the next aide launch. For non-tmpfs systems (macOS), register an `atexit`-style cleanup or use a wrapper approach:

**Important design note on cleanup with `syscall.Exec`:** Since `syscall.Exec` replaces the process, deferred cleanup will not run. Two approaches:

1. **Preferred (simple):** Accept that on macOS (where runtime dir is in `/tmp/` not tmpfs), stale dirs accumulate until the next aide launch calls `CleanStale()`. The `/tmp/` directory is cleaned periodically by the OS anyway.
2. **Alternative (robust):** Instead of `syscall.Exec`, use `os/exec.Command` with `Start()` + `Wait()`, then clean up after the agent exits. This keeps aide as a parent process. The trade-off is that aide stays resident in memory and signals require forwarding.

Start with approach 1 (`syscall.Exec` + `CleanStale`) for simplicity. If stale cleanup becomes a problem, switch to approach 2.

#### Step 9: Cleanup

- `defer runtimeDir.Cleanup()` at the top of the function. This handles error-exit paths (before `syscall.Exec` is reached).
- `runtimeDir.RegisterSignalHandlers()` for signal-based cleanup during the setup phase.
- After `syscall.Exec`, cleanup is handled by `CleanStale` on next launch.

### Verbose Output

When `opts.Verbose` is true, print to stderr (not stdout, to avoid interfering with agent I/O):

```
[aide] Config: ~/.config/aide/config.yaml
[aide] Context: personal (matched via remote github.com/jskswamy/aide)
[aide] Secrets: personal (age key from YubiKey)
[aide] Runtime dir: /run/user/1000/aide-12345/
[aide] Agent: claude (/usr/local/bin/claude)
[aide] Env vars: ANTHROPIC_API_KEY=***, AIDE_CONTEXT=personal
[aide] Args forwarded: --model opus -p "fix bug"
[aide] Launching...
```

Env var values are redacted (shown as `***`) in verbose output. Only the key names are printed.

### TDD Tests

| # | Test | Description |
|---|------|-------------|
| 1 | `TestLaunch_BuildsEnvFromContext` | Provide a config with context env vars (literals only, no secrets). Verify the env slice passed to exec contains the expected vars. (Mock `syscall.Exec` by capturing args.) |
| 2 | `TestLaunch_ResolvesTemplates` | Config with `{{ .secrets.key }}` template. Provide a mock secrets map. Verify resolved value in env. |
| 3 | `TestLaunch_CleanEnvMinimal` | Set `CleanEnv: true`. Verify only minimal env vars + aide-injected vars are in the env slice. |
| 4 | `TestLaunch_InheritsEnvByDefault` | Set `CleanEnv: false`. Verify current process env vars are included in the output. |
| 5 | `TestLaunch_ForwardsArgs` | Set `AgentArgs: ["--model", "opus", "-p", "fix bug"]`. Verify they appear in the argv after the binary name. |
| 6 | `TestLaunch_NoSecretsFile` | Context without `secret`. Verify decryption is skipped, literal env values work. |
| 7 | `TestLaunch_AgentBinaryResolution` | Config with `agents.claude.binary: claude-code`. Verify `exec.LookPath("claude-code")` is called. |
| 8 | `TestLaunch_AgentBinaryNotFound` | Agent binary not on PATH. Verify error with install guidance. |
| 9 | `TestLaunch_RuntimeDirCreated` | Verify `NewRuntimeDir()` is called and the path is set in env as `AIDE_RUNTIME_DIR`. |
| 10 | `TestLaunch_VerboseOutput` | Set `Verbose: true`. Capture stderr. Verify resolution steps are printed with redacted secrets. |

**Testing strategy for `syscall.Exec`:** Since `syscall.Exec` replaces the process, extract the exec call behind an interface:

```go
// Execer abstracts process execution for testing.
type Execer interface {
    Exec(binary string, argv []string, env []string) error
}

// RealExecer calls syscall.Exec.
type RealExecer struct{}

func (RealExecer) Exec(binary string, argv []string, env []string) error {
    return syscall.Exec(binary, argv, env)
}
```

Tests inject a mock `Execer` that captures the arguments instead of exec-ing.

### Verification

```bash
go test ./internal/launcher/ -run TestLaunch -v
```

---

## Task 11: Zero-Config Passthrough

**File:** `internal/launcher/zeroconfig.go`
**Test file:** `internal/launcher/zeroconfig_test.go`

### Purpose

When no aide config file exists, detect known agent binaries on PATH and launch the appropriate one directly. This ensures aide adds zero overhead for simple setups and never makes things worse for users who do not need multi-context management (DD-7).

### Known Agents

Hardcoded list of known agent binaries and their associated API key environment variables:

```go
var knownAgents = []struct {
    Binary string
    EnvKey string // API key env var (checked for "ready to use" detection)
}{
    {Binary: "claude", EnvKey: "ANTHROPIC_API_KEY"},
    {Binary: "gemini", EnvKey: "GEMINI_API_KEY"},
    {Binary: "codex",  EnvKey: "OPENAI_API_KEY"},
}
```

### Interface

```go
package launcher

// ZeroConfigResult describes the outcome of zero-config detection.
type ZeroConfigResult struct {
    // Agent is the detected agent binary (empty if none found).
    Agent string
    // BinaryPath is the absolute path to the agent binary.
    BinaryPath string
    // Ready is true if the agent's API key is already in the environment.
    Ready bool
}

// DetectAgents scans PATH for known agent binaries.
// Returns all detected agents.
func DetectAgents() []ZeroConfigResult

// ZeroConfigLaunch attempts to launch an agent without any config.
// Returns an error if it cannot determine which agent to use.
// Does not return on success (calls syscall.Exec).
func ZeroConfigLaunch(opts *LaunchOptions) error
```

### Decision Logic

```
DetectAgents()
  |
  +-- 0 agents found:
  |     Error: "No known agent binaries found on PATH.
  |             Install one of: claude, gemini, codex.
  |             Or create ~/.config/aide/config.yaml for custom agents."
  |
  +-- 1 agent found:
  |     +-- Has API key in env: exec immediately (pure passthrough)
  |     +-- No API key: exec anyway (agent will handle auth prompts)
  |
  +-- Multiple agents found:
  |     +-- opts.AgentOverride set: use that agent
  |     +-- No override:
  |           Error: "Multiple agents found: claude, codex.
  |                   Use 'aide --agent claude' to pick one,
  |                   or run 'aide setup' to configure."
  |
  [On first-run (no config + successful launch)]:
        Print hint to stderr:
        "[aide] Tip: Run 'aide setup' to configure contexts, secrets, and MCP servers."
```

### Implementation Notes

- Use `exec.LookPath(binary)` for each known agent to check PATH.
- The "Ready" check (`os.Getenv(envKey) != ""`) is informational. Even if the API key is not set, still launch the agent -- it may handle authentication itself (e.g., OAuth flow).
- When launching, use `syscall.Exec` directly (no runtime dir, no secrets, no template resolution needed). The zero-config path skips all of that.
- The `--agent` flag from `LaunchOptions.AgentOverride` resolves the ambiguity when multiple agents are found. Check that the override is in the known agents list OR on PATH.
- The first-run hint is printed to stderr exactly once per launch. It should not be printed if aide has a config file (that case is handled by the normal launch path).

### Integration with Main Command

The root command (`cmd/aide/main.go`) should check for config existence first:

```go
func rootRun(cmd *cobra.Command, args []string) error {
    cfg, err := config.Load()
    if errors.Is(err, config.ErrNoConfig) {
        // No config file found -- try zero-config passthrough
        return launcher.ZeroConfigLaunch(&launcher.LaunchOptions{
            AgentOverride: agentFlag,
            AgentArgs:     args,
        })
    }
    if err != nil {
        return err
    }
    // Normal launch path
    return launcher.Launch(cfg, &launcher.LaunchOptions{...})
}
```

### TDD Tests

| # | Test | Description |
|---|------|-------------|
| 1 | `TestDetectAgents_SingleAgent` | Put a fake `claude` binary on a temp PATH. Verify one result returned with correct binary name and path. |
| 2 | `TestDetectAgents_MultipleAgents` | Put fake `claude` and `codex` on PATH. Verify two results. |
| 3 | `TestDetectAgents_NoAgents` | Empty PATH. Verify empty result. |
| 4 | `TestDetectAgents_Ready` | Put fake `claude` on PATH, set `ANTHROPIC_API_KEY`. Verify `Ready: true`. |
| 5 | `TestDetectAgents_NotReady` | Put fake `claude` on PATH, unset `ANTHROPIC_API_KEY`. Verify `Ready: false`. |
| 6 | `TestZeroConfigLaunch_SingleAgent` | One agent on PATH. Verify `syscall.Exec` is called with that binary. |
| 7 | `TestZeroConfigLaunch_MultipleAgentsNoOverride` | Two agents on PATH, no `--agent` flag. Verify error message lists both agents. |
| 8 | `TestZeroConfigLaunch_MultipleAgentsWithOverride` | Two agents on PATH, `AgentOverride: "claude"`. Verify correct binary is exec'd. |
| 9 | `TestZeroConfigLaunch_NoAgents` | No agents on PATH. Verify error with install guidance. |
| 10 | `TestZeroConfigLaunch_ForwardsArgs` | Single agent, `AgentArgs: ["-p", "hello"]`. Verify args appear in argv. |
| 11 | `TestZeroConfigLaunch_FirstRunHint` | Capture stderr. Verify "Run 'aide setup'" hint is printed. |
| 12 | `TestZeroConfigLaunch_OverrideNotOnPath` | `AgentOverride: "nonexistent"`. Verify error with helpful message. |

### Verification

```bash
go test ./internal/launcher/ -run "TestDetectAgents|TestZeroConfigLaunch" -v
```

---

## Cross-Cutting Concerns

### Error Handling Strategy

All errors in this epic follow a consistent pattern:

1. Wrap with context: `fmt.Errorf("failed to decrypt %s: %w", path, err)`
2. Include actionable guidance in user-facing errors (what to do next)
3. Never expose raw sops/age library errors directly to the user without context

### Testing Infrastructure

- **Test age key:** A dedicated age key pair committed to `internal/secrets/testdata/` for sops integration tests. This key protects no real secrets.
- **Mock Execer:** All tests that would call `syscall.Exec` use the `Execer` interface to capture arguments.
- **Temp PATH manipulation:** Tests that check binary discovery create temp directories with fake executables and prepend them to PATH using `t.Setenv`.
- **XDG overrides:** Tests that depend on XDG paths use `t.Setenv("XDG_CONFIG_HOME", ...)` and `t.Setenv("XDG_RUNTIME_DIR", ...)` to isolate from the real user environment.

### File Summary

| File | Task | Purpose |
|------|------|---------|
| `internal/secrets/age.go` | 6 | Age key discovery (YubiKey, env, file) |
| `internal/secrets/age_test.go` | 6 | Tests for age key discovery |
| `internal/secrets/sops.go` | 7 | Sops library decryption to map[string]string |
| `internal/secrets/sops_test.go` | 7 | Tests for sops decryption |
| `internal/secrets/testdata/test-key.txt` | 7 | Test-only age private key |
| `internal/secrets/testdata/test-key.pub` | 7 | Test-only age public key |
| `internal/secrets/testdata/test.enc.yaml` | 7 | Sops-encrypted test fixture |
| `internal/config/template.go` | 8 | Template resolution with text/template |
| `internal/config/template_test.go` | 8 | Tests for template resolution |
| `internal/launcher/runtime.go` | 9 | Ephemeral runtime dir management |
| `internal/launcher/runtime_test.go` | 9 | Tests for runtime dir lifecycle |
| `internal/launcher/launcher.go` | 10 | Agent launcher orchestration |
| `internal/launcher/launcher_test.go` | 10 | Tests for launcher |
| `internal/launcher/zeroconfig.go` | 11 | Zero-config passthrough detection |
| `internal/launcher/zeroconfig_test.go` | 11 | Tests for zero-config |

### Go Module Dependency

Add to `go.mod`:

```
require github.com/getsops/sops/v3 v3.x.x
```

This pulls in the sops decrypt library. The sops module transitively brings in age and other crypto dependencies. No other new external dependencies are needed for this epic (text/template is stdlib, syscall is stdlib).

### Full Verification (All Tasks)

```bash
go test ./internal/secrets/... ./internal/config/... ./internal/launcher/... -v
```
