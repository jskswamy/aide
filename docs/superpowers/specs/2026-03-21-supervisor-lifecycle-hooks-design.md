# Supervisor Model with Lifecycle Hooks

**Date:** 2026-03-21
**Status:** Draft

## Problem

Tools invoked inside the seatbelt sandbox (e.g., beads/dolt) may need capabilities the sandbox denies. The specific failure: `dolt sql-server` needs `network-bind` to listen on a TCP port, but the deny-default seatbelt profile blocks this. Since dolt picks ephemeral ports and multiple project-specific dolt instances may run concurrently, we cannot whitelist ports upfront.

This is not dolt-specific. Any tool that binds a port (MCP servers, language servers, dev servers) will hit the same wall.

## Constraints

- Outbound TCP connections work from inside the sandbox (only `bind` is blocked).
- Unix domain sockets are also blocked by the sandbox.
- The current `syscall.Exec` model replaces the aide process, leaving no parent to run cleanup or provide runtime control.
- Existing tests use a `mockExecer` that captures exec args. The new model must preserve this testability.

## Design

### 1. Supervisor Process Model

Replace `syscall.Exec` with `exec.Command` + `Wait`. Aide stays alive as the parent (supervisor) process.

**Current flow:**
```
aide → syscall.Exec → sandbox-exec agent  (aide process replaced)
```

**New flow:**
```
aide (supervisor)
  ├── run before-hooks (outside sandbox)
  ├── spawn agent subprocess (inside sandbox)
  │     └── sandbox-exec -f profile.sb agent [args]
  ├── forward signals, wait for exit
  ├── run after-hooks (outside sandbox)
  └── exit with agent's exit code
```

**Process responsibilities:**
- **stdin/stdout/stderr**: Inherited directly by the child process (no proxying needed — `cmd.Stdin/Stdout/Stderr = os.Stdin/Stdout/Stderr`).
- **Signal forwarding**: Supervisor catches SIGINT, SIGTERM, SIGQUIT, SIGHUP and forwards them to the child process group.
- **Exit code propagation**: Supervisor exits with the child's exit code.

### 2. Execer Interface Evolution

The existing `Execer` interface changes from fire-and-forget to lifecycle-aware:

```go
// Current interface (replaced)
type Execer interface {
    Exec(binary string, args []string, env []string) error
}

// New interface
type Execer interface {
    // Run starts the agent process, waits for it to exit, and returns
    // the exit code. Returns -1 and an error if the process could not
    // be started.
    Run(binary string, args []string, env []string) (exitCode int, err error)
}
```

**Implementations:**

- `SubprocessExecer` (production): spawns `exec.Command`, wires stdio, forwards signals, waits, returns exit code.
- `MockExecer` (tests): captures args like before, but returns `(int, error)` instead of `error`:
  ```go
  type mockExecer struct {
      binary   string
      args     []string
      env      []string
      exitCode int   // configured return value
      err      error // configured return error
  }
  func (m *mockExecer) Run(binary string, args []string, env []string) (int, error) {
      m.binary = binary
      m.args = args
      m.env = env
      return m.exitCode, m.err
  }
  ```
  All existing test call sites that check `m.binary`, `m.args`, `m.env` continue to work. Tests that checked `m.err` now also check `m.exitCode`.

The `SyscallExecer` is removed. If someone needs the old behavior (e.g., a no-sandbox passthrough mode that wants zero overhead), they can set `sandbox: false` and use the subprocess model — the overhead of a parent process is negligible.

### 3. Exit Code Propagation

Both `Launcher.Launch()` and `Launcher.Passthrough()` currently return `error`. To propagate exit codes without changing the return signature across the call chain, introduce an `ExitError` sentinel type:

```go
// ExitError wraps a non-zero exit code from the agent process.
// The caller (main.go) checks for this and calls os.Exit(code).
type ExitError struct {
    Code int
}

func (e *ExitError) Error() string {
    return fmt.Sprintf("agent exited with code %d", e.Code)
}
```

In `main.go`, the top-level caller unwraps:

```go
err := launcher.Launch(...)
var exitErr *launcher.ExitError
if errors.As(err, &exitErr) {
    os.Exit(exitErr.Code)
}
if err != nil {
    // handle other errors
}
```

This preserves the existing `error` return signature while correctly propagating exit codes.

### 4. Unified Signal Handler

The existing `RuntimeDir.RegisterSignalHandlers()` catches SIGINT/SIGTERM/SIGQUIT/SIGHUP and cleans up the runtime dir. The supervisor needs the same signals to forward to the child and run after-hooks. **These must not compete.**

**Resolution:** Remove `RuntimeDir.RegisterSignalHandlers()`. The supervisor owns signal handling with a single handler that does, in order:

1. Forward the signal to the child process group.
2. Wait for child to exit (with a short timeout).
3. Run after-hooks (with a 30s collective timeout during signal cleanup — see section 5).
4. Clean up the runtime dir.
5. Re-raise the signal for default behavior.

`RuntimeDir` keeps its `Cleanup()` method but loses `RegisterSignalHandlers()`. The `Launcher` is now responsible for both signal handling and cleanup.

For the passthrough path (no hooks), the supervisor's signal handler still follows the same flow but steps 3 is a no-op.

### 5. Context-Level Lifecycle Hooks

Hooks are one-shot shell commands declared per context. They run outside the sandbox, in the user's normal shell environment.

**Config schema additions:**

```go
// Added to Context:
type Context struct {
    // ... existing fields ...
    Hooks *Hooks `yaml:"hooks,omitempty"`
}

// Added to top-level Config (for minimal format):
type Config struct {
    // ... existing fields ...
    Hooks *Hooks `yaml:"hooks,omitempty"` // minimal format only
}

// New types:
type Hooks struct {
    Before []Hook `yaml:"before,omitempty"`
    After  []Hook `yaml:"after,omitempty"`
}

type Hook struct {
    Name     string `yaml:"name"`
    Run      string `yaml:"run"`
    Required *bool  `yaml:"required,omitempty"` // default: true
}
```

`normalizeMinimal()` must copy `cfg.Hooks` into the synthesized default context:

```go
func normalizeMinimal(cfg *Config) *Config {
    return &Config{
        // ... existing fields ...
        Contexts: map[string]Context{
            "default": {
                // ... existing fields ...
                Hooks: cfg.Hooks,
            },
        },
    }
}
```

Project overrides (`.aide.yaml`) **cannot** declare hooks. Hooks configure external services that depend on the machine's environment, not the project. Adding hooks to project overrides would mean a cloned repo could silently run arbitrary commands outside the sandbox — a security concern.

**YAML examples:**

Full format:
```yaml
contexts:
  aide-project:
    match:
      - remote: "github.com/jskswamy/aide"
    agent: claude
    hooks:
      before:
        - name: dolt-server
          run: "bd dolt start"
        - name: warm-cache
          run: "some-script --prep"
          required: false
      after:
        - name: dolt-server
          run: "bd dolt stop"
```

Minimal format:
```yaml
agent: claude
hooks:
  before:
    - name: dolt-server
      run: "bd dolt start"
  after:
    - name: dolt-server
      run: "bd dolt stop"
```

### 6. Hook Execution Semantics

**Before-hooks:**
- Run sequentially in declaration order, before the agent subprocess starts.
- Run in the user's shell environment (same env the agent will inherit, minus sandbox).
- Working directory is the project root.
- If a required hook (default) fails (non-zero exit), abort launch with the hook's stderr/stdout as the error message.
- If an optional hook (`required: false`) fails, log a warning to stderr and continue.
- Hooks must return within the 30-second timeout. If a hook needs to start a background service (e.g., `bd dolt start` daemonizes dolt and returns), that is the hook command's responsibility. A hook that blocks (runs a server in the foreground) will be killed after 30s and treated as a failure.

**After-hooks:**
- Run sequentially in declaration order, after the agent process exits.
- Run regardless of how the agent exited (clean exit, error, or signal).
- After-hook failures are logged as warnings but do not change the exit code.
- **Timeout asymmetry (deliberate):** On normal exit, each after-hook gets 30s individually (there's no urgency). On signal-triggered cleanup, after-hooks share a collective 30-second timeout (not 30s each) because the OS or container orchestrator may forcibly kill the process if cleanup takes too long.

**Hook output routing:** Hook stdout and stderr are both forwarded to the supervisor's stderr. This is deliberate: the agent subprocess owns stdin/stdout for its terminal I/O. Hook diagnostic output must not interfere with the agent's stream. Implementers should not "fix" this by routing hook stdout to os.Stdout.

**Hook runner interface:**

```go
type HookRunner interface {
    RunHook(hook Hook, env []string, dir string) error
}
```

The `Launcher` struct gets a `HookRunner` field for testability (like it has `Execer`):

```go
type Launcher struct {
    Execer     Execer
    HookRunner HookRunner  // nil → DefaultHookRunner
    // ... existing fields ...
}
```

`DefaultHookRunner` executes hooks via `exec.Command("sh", "-c", hook.Run)` with inherited environment and project root as working directory.

### 7. Stale Runtime Directory Cleanup

The existing `CleanStale()` mechanism already handles this:
- On startup, enumerate `aide-*` dirs in `$XDG_RUNTIME_DIR`.
- Parse PID from directory name.
- Check if process is alive via `kill -0`.
- Remove directories for dead processes.

With the supervisor model, the aide process stays alive for the session duration, so `isProcessAlive(pid)` correctly identifies active sessions. No changes needed to `CleanStale()`.

### 8. Control Socket (Future)

The supervisor model enables a future control socket at `$XDG_RUNTIME_DIR/aide-<pid>/control.sock`. This is **out of scope** for this design but the supervisor architecture is prerequisite. Future capabilities:
- Runtime sandbox reconfiguration (e.g., toggle `kubectl delete` permission).
- Session management (rename, save).
- HTTPS proxy control.

### 9. Launch Flow (Updated)

The `Launcher.Launch()` method changes at step 14:

```
Steps 1-13: unchanged (config, context, secrets, env, sandbox, banner)

Step 14 (NEW): Run before-hooks
  - For each hook in context.Hooks.Before:
    - Execute hook.Run via sh -c (30s timeout per hook)
    - If required hook fails: cleanup runtime dir, return error
    - If optional hook fails: log warning, continue

Step 15 (CHANGED): Run agent as subprocess
  - Call Execer.Run(binary, args, env)
  - Supervisor waits for agent to exit
  - Signal handler: forward signal → wait for child → run after-hooks → cleanup

Step 16 (NEW): Run after-hooks (on normal exit)
  - For each hook in context.Hooks.After:
    - Execute hook.Run via sh -c (30s timeout per hook)
    - Log warnings on failure

Step 17 (NEW): Cleanup and exit
  - Clean up runtime dir
  - Return ExitError with agent's exit code (or nil for exit code 0)
```

The `Passthrough` path gets the same supervisor treatment (subprocess instead of exec) but has no hooks (no config to declare them in).

### 10. Testing Strategy

This is an architectural change. Use superpowered TDD to ensure no existing functionality breaks.

**Unit tests (new):**
- `SubprocessExecer`: verify it starts a process, forwards signals, returns exit code.
- `HookRunner`: verify sequential execution, required vs optional failure handling, timeout behavior.
- `Launcher.Launch()` with hooks: verify before-hooks run before agent, after-hooks run after, required hook failure aborts launch.
- `ExitError`: verify it's correctly created, unwrapped, and returned.

**Unit tests (updated):**
- All existing `Launcher` tests switch from `SyscallExecer` to the new `MockExecer` that returns exit codes. The mock interface changes but the test patterns (capture args, verify sandbox wrapping) remain the same.
- `RuntimeDir` tests: remove tests for `RegisterSignalHandlers` since it moves to the supervisor.

**Integration tests:**
- End-to-end: aide launches a test binary with before/after hooks, verify hooks run in order.
- Hook failure: before-hook fails, verify agent never starts.
- Signal handling: send SIGTERM to supervisor, verify after-hooks run and child is terminated.

### 11. Passthrough Path

The `execAgent` function in `passthrough.go` also uses `syscall.Exec` today. It changes to use the same `Execer.Run()` subprocess model. Since passthrough has no config, there are no hooks — but the supervisor stays alive for signal handling, cleanup, and future control socket support. The function's return type stays `error`, using `ExitError` for non-zero exit codes.

### 12. Banner Changes

The startup banner should indicate when hooks are configured. Add a "Hooks" line showing the count of before/after hooks, similar to how sandbox info is displayed today. This is a minor UI addition.

## What This Does NOT Include

- **HTTPS proxy**: Built into the supervisor in a future design, not a hook.
- **Control socket API**: Future work. This design just enables it.
- **Agent-level hooks**: Dropped per design discussion. Context-level only.
- **Project override hooks**: `.aide.yaml` cannot declare hooks (security boundary).
- **Service/daemon hooks**: One-shot only. Long-running service management deferred.
- **Hook timeout configuration**: Fixed at 30s per hook (before), 30s collective (after on signal). Configurable if needed later.
