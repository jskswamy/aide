# Supervisor Lifecycle Hooks Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace `syscall.Exec` with a supervisor subprocess model and add before/after lifecycle hooks to contexts.

**Architecture:** The `Execer` interface changes from `Exec() error` to `Run() (int, error)`. A new `SubprocessExecer` spawns the agent as a child process, forwards signals, and returns the exit code. Hooks run via a `HookRunner` interface before/after the agent subprocess. Signal handling is unified in the supervisor (removing `RuntimeDir.RegisterSignalHandlers`).

**Tech Stack:** Go stdlib (`os/exec`, `os/signal`, `syscall`, `context`), existing test patterns with `mockExecer`.

**Spec:** `docs/superpowers/specs/2026-03-21-supervisor-lifecycle-hooks-design.md`

---

### Task 1: ExitError Sentinel Type

**Files:**
- Create: `internal/launcher/exit_error.go`
- Create: `internal/launcher/exit_error_test.go`

- [ ] **Step 1: Write failing tests for ExitError**

```go
// exit_error_test.go
package launcher

import (
    "errors"
    "testing"
)

func TestExitError_Error(t *testing.T) {
    e := &ExitError{Code: 42}
    want := "agent exited with code 42"
    if e.Error() != want {
        t.Errorf("Error() = %q, want %q", e.Error(), want)
    }
}

func TestExitError_Unwrap(t *testing.T) {
    e := &ExitError{Code: 1}
    var target *ExitError
    if !errors.As(e, &target) {
        t.Fatal("errors.As failed")
    }
    if target.Code != 1 {
        t.Errorf("Code = %d, want 1", target.Code)
    }
}

func TestExitError_ZeroCodeNil(t *testing.T) {
    // Helper: NewExitError returns nil for code 0
    err := NewExitError(0)
    if err != nil {
        t.Errorf("NewExitError(0) = %v, want nil", err)
    }
}

func TestExitError_NonZeroCode(t *testing.T) {
    err := NewExitError(1)
    if err == nil {
        t.Fatal("NewExitError(1) = nil, want error")
    }
    var exitErr *ExitError
    if !errors.As(err, &exitErr) {
        t.Fatal("expected ExitError type")
    }
    if exitErr.Code != 1 {
        t.Errorf("Code = %d, want 1", exitErr.Code)
    }
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && go test ./internal/launcher/ -run TestExitError -v`
Expected: FAIL — `ExitError` not defined

- [ ] **Step 3: Implement ExitError**

```go
// exit_error.go
package launcher

import "fmt"

// ExitError wraps a non-zero exit code from the agent process.
// The caller (main.go) checks for this and calls os.Exit(code).
type ExitError struct {
    Code int
}

func (e *ExitError) Error() string {
    return fmt.Sprintf("agent exited with code %d", e.Code)
}

// NewExitError returns nil for exit code 0, or an ExitError for non-zero codes.
func NewExitError(code int) error {
    if code == 0 {
        return nil
    }
    return &ExitError{Code: code}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && go test ./internal/launcher/ -run TestExitError -v`
Expected: PASS

- [ ] **Step 5: Commit**

```
git add internal/launcher/exit_error.go internal/launcher/exit_error_test.go
```
Message: `Add ExitError sentinel type for exit code propagation`

---

### Task 2: Evolve Execer Interface

**Files:**
- Modify: `internal/launcher/launcher.go` (Execer interface, remove SyscallExecer)
- Create: `internal/launcher/subprocess.go` (SubprocessExecer)
- Create: `internal/launcher/subprocess_test.go`
- Modify: `internal/launcher/launcher_test.go` (update mockExecer)
- Modify: `internal/launcher/passthrough_test.go` (update mockExecer usage)

- [ ] **Step 1: Write failing tests for SubprocessExecer**

```go
// subprocess_test.go
package launcher

import (
    "os"
    "os/exec"
    "runtime"
    "testing"
)

func TestSubprocessExecer_RunsAndReturnsExitCode(t *testing.T) {
    // Use a shell command that exits with a known code
    e := &SubprocessExecer{}
    code, err := e.Run("/bin/sh", []string{"/bin/sh", "-c", "exit 0"}, os.Environ())
    if err != nil {
        t.Fatalf("Run error: %v", err)
    }
    if code != 0 {
        t.Errorf("exit code = %d, want 0", code)
    }
}

func TestSubprocessExecer_NonZeroExit(t *testing.T) {
    e := &SubprocessExecer{}
    code, err := e.Run("/bin/sh", []string{"/bin/sh", "-c", "exit 42"}, os.Environ())
    if err != nil {
        t.Fatalf("Run error: %v", err)
    }
    if code != 42 {
        t.Errorf("exit code = %d, want 42", code)
    }
}

func TestSubprocessExecer_InheritsStdio(t *testing.T) {
    // Verify the subprocess can write to stdout (inherited)
    e := &SubprocessExecer{}
    code, err := e.Run("/bin/sh", []string{"/bin/sh", "-c", "echo hello >/dev/null"}, os.Environ())
    if err != nil {
        t.Fatalf("Run error: %v", err)
    }
    if code != 0 {
        t.Errorf("exit code = %d, want 0", code)
    }
}

func TestSubprocessExecer_BinaryNotFound(t *testing.T) {
    e := &SubprocessExecer{}
    code, err := e.Run("/nonexistent/binary", []string{"/nonexistent/binary"}, os.Environ())
    if err == nil {
        t.Fatal("expected error for nonexistent binary")
    }
    if code != -1 {
        t.Errorf("exit code = %d, want -1", code)
    }
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && go test ./internal/launcher/ -run TestSubprocessExecer -v`
Expected: FAIL — `SubprocessExecer` not defined

- [ ] **Step 3: Implement SubprocessExecer**

```go
// subprocess.go
package launcher

import (
    "fmt"
    "os"
    "os/exec"
    "os/signal"
    "syscall"
)

// SubprocessExecer runs the agent as a child process, inheriting stdio
// and forwarding signals. It waits for the child to exit and returns the
// exit code.
type SubprocessExecer struct{}

// Run starts the agent process, forwards signals, waits for exit, and
// returns the exit code. Returns -1 and an error if the process could
// not be started.
func (s *SubprocessExecer) Run(binary string, args []string, env []string) (int, error) {
    cmd := exec.Command(binary, args[1:]...)
    cmd.Stdin = os.Stdin
    cmd.Stdout = os.Stdout
    cmd.Stderr = os.Stderr
    cmd.Env = env
    // Set process group so we can forward signals to the child
    cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

    if err := cmd.Start(); err != nil {
        return -1, fmt.Errorf("starting agent: %w", err)
    }

    // Forward signals to the child process
    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGHUP)
    done := make(chan struct{})

    go func() {
        for {
            select {
            case sig := <-sigCh:
                // Forward signal to the child's process group
                if cmd.Process != nil {
                    _ = syscall.Kill(-cmd.Process.Pid, sig.(syscall.Signal))
                }
            case <-done:
                signal.Stop(sigCh)
                return
            }
        }
    }()

    err := cmd.Wait()
    close(done)

    if err != nil {
        if exitErr, ok := err.(*exec.ExitError); ok {
            return exitErr.ExitCode(), nil
        }
        return -1, fmt.Errorf("waiting for agent: %w", err)
    }
    return 0, nil
}
```

- [ ] **Step 4: Run SubprocessExecer tests**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && go test ./internal/launcher/ -run TestSubprocessExecer -v`
Expected: PASS

- [ ] **Step 5: Update Execer interface and mockExecer**

Change the `Execer` interface in `launcher.go`:

```go
// Old:
type Execer interface {
    Exec(binary string, args []string, env []string) error
}

// New:
type Execer interface {
    Run(binary string, args []string, env []string) (exitCode int, err error)
}
```

Remove `SyscallExecer` from `launcher.go`.

Update `mockExecer` in `launcher_test.go`:

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

- [ ] **Step 6: Update Launch() and execAgent() to use Run()**

In `launcher.go`, change the final exec call (line 237-238):

```go
// Old:
args := append([]string{binary}, extraArgs...)
return l.Execer.Exec(binary, args, env)

// New:
args := append([]string{binary}, extraArgs...)
exitCode, err := l.Execer.Run(binary, args, env)
if err != nil {
    return fmt.Errorf("running agent: %w", err)
}
return NewExitError(exitCode)
```

In `passthrough.go`, change `execAgent()` (line 165). Also add `defer rtDir.Cleanup()` after creating the runtime dir (since the supervisor now stays alive, it must clean up):

```go
// After rtDir creation, add:
defer rtDir.Cleanup()

// Then at the end, replace:
// Old:
return l.Execer.Exec(cmd.Path, cmd.Args, cmd.Env)

// New:
exitCode, err := l.Execer.Run(cmd.Path, cmd.Args, cmd.Env)
if err != nil {
    return fmt.Errorf("running agent: %w", err)
}
return NewExitError(exitCode)
```

- [ ] **Step 7: Update main.go**

Replace `&launcher.SyscallExecer{}` with `&launcher.SubprocessExecer{}` on line 43:

```go
// Old:
Execer: &launcher.SyscallExecer{},

// New:
Execer: &launcher.SubprocessExecer{},
```

Add `SilenceErrors: true` to the root command (prevents Cobra from printing `ExitError` messages):

```go
rootCmd := &cobra.Command{
    // ... existing fields ...
    SilenceErrors: true,
}
```

Update the error handling after `rootCmd.Execute()`:

```go
if err := rootCmd.Execute(); err != nil {
    var exitErr *launcher.ExitError
    if errors.As(err, &exitErr) {
        os.Exit(exitErr.Code)
    }
    fmt.Fprintln(os.Stderr, err)
    os.Exit(1)
}
```

Add `"errors"` to imports.

- [ ] **Step 8: Run all existing tests to verify nothing breaks**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && go test ./internal/launcher/ -v`
Expected: ALL PASS. Existing tests should work with the new mock interface since they only check `mock.binary`, `mock.args`, `mock.env` — which are unchanged.

Note: Tests that check `if err := l.Launch(...); err != nil` will now get `ExitError{Code: 0}` which is `nil` (via `NewExitError`), so they should still pass. Tests that verify `mock.binary != ""` to check exec was called continue to work since `mockExecer.Run` still sets `m.binary`.

- [ ] **Step 9: Commit**

```
git add internal/launcher/subprocess.go internal/launcher/subprocess_test.go internal/launcher/launcher.go internal/launcher/launcher_test.go internal/launcher/passthrough.go internal/launcher/passthrough_test.go cmd/aide/main.go
```
Message: `Replace syscall.Exec with subprocess model`

---

### Task 3: Config Schema — Hooks Types

**Files:**
- Modify: `internal/config/schema.go`
- Create: `internal/config/hooks_test.go`

- [ ] **Step 1: Write failing tests for Hooks YAML parsing**

```go
// hooks_test.go
package config

import (
    "testing"

    "gopkg.in/yaml.v3"
)

func TestHooks_UnmarshalYAML(t *testing.T) {
    input := `
hooks:
  before:
    - name: dolt-server
      run: "bd dolt start"
    - name: warm-cache
      run: "some-script"
      required: false
  after:
    - name: dolt-server
      run: "bd dolt stop"
`
    var ctx Context
    if err := yaml.Unmarshal([]byte(input), &ctx); err != nil {
        t.Fatalf("unmarshal: %v", err)
    }
    if ctx.Hooks == nil {
        t.Fatal("Hooks is nil")
    }
    if len(ctx.Hooks.Before) != 2 {
        t.Fatalf("Before hooks: got %d, want 2", len(ctx.Hooks.Before))
    }
    if ctx.Hooks.Before[0].Name != "dolt-server" {
        t.Errorf("Before[0].Name = %q, want dolt-server", ctx.Hooks.Before[0].Name)
    }
    if ctx.Hooks.Before[0].Run != "bd dolt start" {
        t.Errorf("Before[0].Run = %q", ctx.Hooks.Before[0].Run)
    }
    // First hook: required should be nil (defaults to true)
    if ctx.Hooks.Before[0].Required != nil {
        t.Errorf("Before[0].Required = %v, want nil", *ctx.Hooks.Before[0].Required)
    }
    // Second hook: required explicitly false
    if ctx.Hooks.Before[1].Required == nil || *ctx.Hooks.Before[1].Required {
        t.Errorf("Before[1].Required should be false")
    }
    if len(ctx.Hooks.After) != 1 {
        t.Fatalf("After hooks: got %d, want 1", len(ctx.Hooks.After))
    }
}

func TestHook_IsRequired(t *testing.T) {
    // nil Required → true (default)
    h1 := Hook{Name: "a", Run: "echo"}
    if !h1.IsRequired() {
        t.Error("nil Required should default to true")
    }
    // explicit true
    tr := true
    h2 := Hook{Name: "b", Run: "echo", Required: &tr}
    if !h2.IsRequired() {
        t.Error("explicit true should be required")
    }
    // explicit false
    fa := false
    h3 := Hook{Name: "c", Run: "echo", Required: &fa}
    if h3.IsRequired() {
        t.Error("explicit false should not be required")
    }
}

func TestHooks_MinimalConfig(t *testing.T) {
    input := `
agent: claude
hooks:
  before:
    - name: start-db
      run: "db start"
`
    var cfg Config
    if err := yaml.Unmarshal([]byte(input), &cfg); err != nil {
        t.Fatalf("unmarshal: %v", err)
    }
    if cfg.Hooks == nil {
        t.Fatal("top-level Hooks is nil")
    }
    if len(cfg.Hooks.Before) != 1 {
        t.Fatalf("Before hooks: got %d, want 1", len(cfg.Hooks.Before))
    }
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && go test ./internal/config/ -run "TestHook" -v`
Expected: FAIL — `Hooks`, `Hook` types not defined

- [ ] **Step 3: Add types to schema.go**

Add to `schema.go`:

```go
// Hooks defines lifecycle hooks for a context.
type Hooks struct {
    Before []Hook `yaml:"before,omitempty"`
    After  []Hook `yaml:"after,omitempty"`
}

// Hook is a one-shot shell command that runs outside the sandbox.
type Hook struct {
    Name     string `yaml:"name"`
    Run      string `yaml:"run"`
    Required *bool  `yaml:"required,omitempty"` // default: true
}

// IsRequired returns true if the hook is required (default when Required is nil).
func (h *Hook) IsRequired() bool {
    return h.Required == nil || *h.Required
}
```

Add `Hooks *Hooks` field to the `Context` struct:

```go
type Context struct {
    // ... existing fields ...
    Hooks *Hooks `yaml:"hooks,omitempty"`
}
```

Add `Hooks *Hooks` field to the top-level `Config` struct (minimal format):

```go
type Config struct {
    // ... existing fields in minimal section ...
    Hooks *Hooks `yaml:"hooks,omitempty"` // minimal format only
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && go test ./internal/config/ -run "TestHook" -v`
Expected: PASS

- [ ] **Step 5: Run all config tests**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && go test ./internal/config/ -v`
Expected: ALL PASS

- [ ] **Step 6: Commit**

```
git add internal/config/schema.go internal/config/hooks_test.go
```
Message: `Add Hooks and Hook types to config schema`

---

### Task 4: Propagate Hooks in normalizeMinimal

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/hooks_test.go` (add normalization test)

- [ ] **Step 1: Write failing test for normalizeMinimal hooks propagation**

Add to `hooks_test.go`:

```go
func TestNormalizeMinimal_PropagatesHooks(t *testing.T) {
    input := `
agent: claude
hooks:
  before:
    - name: start-db
      run: "db start"
  after:
    - name: stop-db
      run: "db stop"
`
    var cfg Config
    if err := yaml.Unmarshal([]byte(input), &cfg); err != nil {
        t.Fatalf("unmarshal: %v", err)
    }

    normalized := normalizeMinimal(&cfg)
    ctx, ok := normalized.Contexts["default"]
    if !ok {
        t.Fatal("no default context after normalization")
    }
    if ctx.Hooks == nil {
        t.Fatal("Hooks not propagated to default context")
    }
    if len(ctx.Hooks.Before) != 1 {
        t.Fatalf("Before hooks: got %d, want 1", len(ctx.Hooks.Before))
    }
    if ctx.Hooks.Before[0].Name != "start-db" {
        t.Errorf("Before[0].Name = %q, want start-db", ctx.Hooks.Before[0].Name)
    }
    if len(ctx.Hooks.After) != 1 {
        t.Fatalf("After hooks: got %d, want 1", len(ctx.Hooks.After))
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && go test ./internal/config/ -run TestNormalizeMinimal_PropagatesHooks -v`
Expected: FAIL — `Hooks` not copied in `normalizeMinimal`

- [ ] **Step 3: Update normalizeMinimal**

In `config.go`, add `Hooks: cfg.Hooks,` to the default context in `normalizeMinimal`:

```go
func normalizeMinimal(cfg *Config) *Config {
    agentName := cfg.Agent
    if agentName == "" {
        agentName = "default"
    }
    return &Config{
        Agents: map[string]AgentDef{
            agentName: {Binary: agentName},
        },
        Contexts: map[string]Context{
            "default": {
                Agent:      agentName,
                Env:        cfg.Env,
                Secret:     cfg.Secret,
                MCPServers: cfg.MCPServers,
                Sandbox:    SandboxPolicyToRef(cfg.Sandbox),
                Hooks:      cfg.Hooks,
            },
        },
        MCP:            cfg.MCP,
        DefaultContext: "default",
        Preferences:    cfg.Preferences,
    }
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && go test ./internal/config/ -run TestNormalizeMinimal_PropagatesHooks -v`
Expected: PASS

- [ ] **Step 5: Run all config tests**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && go test ./internal/config/ -v`
Expected: ALL PASS

- [ ] **Step 6: Commit**

```
git add internal/config/config.go internal/config/hooks_test.go
```
Message: `Propagate hooks through normalizeMinimal`

---

### Task 5: HookRunner Interface and Implementation

**Files:**
- Create: `internal/launcher/hooks.go`
- Create: `internal/launcher/hooks_test.go`

- [ ] **Step 1: Write failing tests for HookRunner**

```go
// hooks_test.go
package launcher

import (
    "os"
    "path/filepath"
    "testing"

    "github.com/jskswamy/aide/internal/config"
)

func TestDefaultHookRunner_Success(t *testing.T) {
    r := &DefaultHookRunner{}
    hook := config.Hook{Name: "test", Run: "true"}
    err := r.RunHook(hook, os.Environ(), t.TempDir())
    if err != nil {
        t.Fatalf("RunHook error: %v", err)
    }
}

func TestDefaultHookRunner_Failure(t *testing.T) {
    r := &DefaultHookRunner{}
    hook := config.Hook{Name: "test", Run: "false"}
    err := r.RunHook(hook, os.Environ(), t.TempDir())
    if err == nil {
        t.Fatal("expected error from failing hook")
    }
}

func TestDefaultHookRunner_WorkingDirectory(t *testing.T) {
    dir := t.TempDir()
    r := &DefaultHookRunner{}
    // Write pwd to a file so we can verify the working dir
    outFile := filepath.Join(dir, "pwd.txt")
    hook := config.Hook{
        Name: "pwd-check",
        Run:  "pwd > " + outFile,
    }
    if err := r.RunHook(hook, os.Environ(), dir); err != nil {
        t.Fatalf("RunHook error: %v", err)
    }
    data, err := os.ReadFile(outFile)
    if err != nil {
        t.Fatalf("read output: %v", err)
    }
    // pwd output should match the dir (may have trailing newline)
    got := string(data)
    if got[:len(got)-1] != dir {
        t.Errorf("working dir = %q, want %q", got, dir)
    }
}

func TestDefaultHookRunner_Timeout(t *testing.T) {
    r := &DefaultHookRunner{Timeout: 1} // 1 second
    hook := config.Hook{Name: "slow", Run: "sleep 30"}
    err := r.RunHook(hook, os.Environ(), t.TempDir())
    if err == nil {
        t.Fatal("expected timeout error")
    }
}

func TestRunBeforeHooks_RequiredFailAborts(t *testing.T) {
    hooks := []config.Hook{
        {Name: "pass", Run: "true"},
        {Name: "fail", Run: "false"},
        {Name: "never", Run: "true"},
    }
    runner := &DefaultHookRunner{}
    err := RunBeforeHooks(hooks, runner, os.Environ(), t.TempDir())
    if err == nil {
        t.Fatal("expected error from required failing hook")
    }
}

func TestRunBeforeHooks_OptionalFailContinues(t *testing.T) {
    fa := false
    hooks := []config.Hook{
        {Name: "optional-fail", Run: "false", Required: &fa},
        {Name: "should-run", Run: "true"},
    }
    runner := &DefaultHookRunner{}
    err := RunBeforeHooks(hooks, runner, os.Environ(), t.TempDir())
    if err != nil {
        t.Fatalf("optional hook failure should not abort: %v", err)
    }
}

func TestRunAfterHooks_FailuresLogged(t *testing.T) {
    hooks := []config.Hook{
        {Name: "cleanup-fail", Run: "false"},
        {Name: "cleanup-pass", Run: "true"},
    }
    runner := &DefaultHookRunner{}
    // After-hooks never return errors (they log warnings)
    RunAfterHooks(hooks, runner, os.Environ(), t.TempDir())
    // No panic, no error — success
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && go test ./internal/launcher/ -run "TestDefaultHookRunner|TestRunBeforeHooks|TestRunAfterHooks" -v`
Expected: FAIL — types not defined

- [ ] **Step 3: Implement HookRunner**

```go
// hooks.go
package launcher

import (
    "context"
    "fmt"
    "log"
    "os"
    "os/exec"
    "time"

    "github.com/jskswamy/aide/internal/config"
)

// HookRunner executes lifecycle hooks.
type HookRunner interface {
    RunHook(hook config.Hook, env []string, dir string) error
}

// DefaultHookRunner executes hooks via sh -c.
type DefaultHookRunner struct {
    Timeout time.Duration // 0 means use default (30s)
}

func (r *DefaultHookRunner) timeout() time.Duration {
    if r.Timeout > 0 {
        return r.Timeout
    }
    return 30 * time.Second
}

// RunHook executes a single hook command. Stdout and stderr are
// forwarded to os.Stderr (to avoid interfering with the agent's stdio).
func (r *DefaultHookRunner) RunHook(hook config.Hook, env []string, dir string) error {
    ctx, cancel := context.WithTimeout(context.Background(), r.timeout())
    defer cancel()

    cmd := exec.CommandContext(ctx, "sh", "-c", hook.Run)
    cmd.Env = env
    cmd.Dir = dir
    cmd.Stdout = os.Stderr // deliberate: hooks output to supervisor's stderr
    cmd.Stderr = os.Stderr

    if err := cmd.Run(); err != nil {
        if ctx.Err() == context.DeadlineExceeded {
            return fmt.Errorf("hook %q timed out after %s", hook.Name, r.timeout())
        }
        return fmt.Errorf("hook %q failed: %w", hook.Name, err)
    }
    return nil
}

// RunBeforeHooks runs before-hooks sequentially. Required hooks abort on
// failure; optional hooks log a warning and continue.
func RunBeforeHooks(hooks []config.Hook, runner HookRunner, env []string, dir string) error {
    for _, h := range hooks {
        if err := runner.RunHook(h, env, dir); err != nil {
            if h.IsRequired() {
                return fmt.Errorf("required before-hook failed: %w", err)
            }
            log.Printf("aide: optional hook %q failed: %v", h.Name, err)
        }
    }
    return nil
}

// RunAfterHooks runs after-hooks sequentially. Failures are logged as
// warnings but never returned as errors.
func RunAfterHooks(hooks []config.Hook, runner HookRunner, env []string, dir string) {
    for _, h := range hooks {
        if err := runner.RunHook(h, env, dir); err != nil {
            log.Printf("aide: after-hook %q failed: %v", h.Name, err)
        }
    }
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && go test ./internal/launcher/ -run "TestDefaultHookRunner|TestRunBeforeHooks|TestRunAfterHooks" -v`
Expected: PASS

- [ ] **Step 5: Commit**

```
git add internal/launcher/hooks.go internal/launcher/hooks_test.go
```
Message: `Add HookRunner interface and DefaultHookRunner implementation`

---

### Task 6: Remove RuntimeDir.RegisterSignalHandlers

**Files:**
- Modify: `internal/launcher/runtime.go`
- Modify: `internal/launcher/runtime_test.go`
- Modify: `internal/launcher/launcher.go` (remove RegisterSignalHandlers call)

- [ ] **Step 1: Remove RegisterSignalHandlers from runtime.go**

Delete the `RegisterSignalHandlers` method (lines 74-98 of `runtime.go`). Keep `Cleanup()`.

Also remove the now-unused imports from `runtime.go`: `"context"`, `"os/signal"`. Keep `"syscall"` (used by `isProcessAlive`), `"log"`, `"sync"`, `"os"`, `"fmt"`, `"path/filepath"`, `"strconv"`, `"strings"`.

- [ ] **Step 2: Remove the call in launcher.go**

In `launcher.go`, remove lines 122-123:

```go
// Remove these two lines:
cancelSignals := rtDir.RegisterSignalHandlers()
defer cancelSignals()
```

- [ ] **Step 3: Run all tests**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && go test ./internal/launcher/ -v`
Expected: ALL PASS. No tests directly test `RegisterSignalHandlers` (it only started a goroutine).

- [ ] **Step 4: Run full project tests**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && go test ./... 2>&1 | tail -20`
Expected: ALL PASS

- [ ] **Step 5: Commit**

```
git add internal/launcher/runtime.go internal/launcher/launcher.go
```
Message: `Remove RuntimeDir.RegisterSignalHandlers — supervisor owns signals`

---

### Task 7: Wire Hooks into Launcher.Launch()

**Files:**
- Modify: `internal/launcher/launcher.go`
- Modify: `internal/launcher/launcher_test.go`

- [ ] **Step 1: Write failing tests for hooks in Launch**

Add to `launcher_test.go`:

```go
// Add "github.com/jskswamy/aide/internal/config" to the imports in launcher_test.go

// mockHookRunner tracks hook execution order.
type mockHookRunner struct {
    executed []string
    failOn   string // hook name that should fail
}

func (m *mockHookRunner) RunHook(hook config.Hook, env []string, dir string) error {
    m.executed = append(m.executed, hook.Name)
    if hook.Name == m.failOn {
        return fmt.Errorf("hook %q failed", hook.Name)
    }
    return nil
}

func TestLauncher_BeforeHooksRun(t *testing.T) {
    configDir := t.TempDir()
    cwd := t.TempDir()
    writeMinimalConfig(t, configDir, `
agent: /usr/local/bin/my-agent
hooks:
  before:
    - name: setup-db
      run: "echo setup"
`)
    hookRunner := &mockHookRunner{}
    mock := &mockExecer{}
    l := &Launcher{
        Execer:     mock,
        ConfigDir:  configDir,
        HookRunner: hookRunner,
    }
    if err := l.Launch(cwd, "", nil, false, false); err != nil {
        t.Fatalf("Launch failed: %v", err)
    }
    if len(hookRunner.executed) != 1 || hookRunner.executed[0] != "setup-db" {
        t.Errorf("expected [setup-db], got %v", hookRunner.executed)
    }
    // Agent should also have been called
    if mock.binary == "" {
        t.Error("agent was not launched")
    }
}

func TestLauncher_RequiredBeforeHookFailAborts(t *testing.T) {
    configDir := t.TempDir()
    cwd := t.TempDir()
    writeMinimalConfig(t, configDir, `
agent: /usr/local/bin/my-agent
hooks:
  before:
    - name: will-fail
      run: "false"
`)
    hookRunner := &mockHookRunner{failOn: "will-fail"}
    mock := &mockExecer{}
    l := &Launcher{
        Execer:     mock,
        ConfigDir:  configDir,
        HookRunner: hookRunner,
    }
    err := l.Launch(cwd, "", nil, false, false)
    if err == nil {
        t.Fatal("expected error from failing required hook")
    }
    // Agent should NOT have been called
    if mock.binary != "" {
        t.Error("agent should not launch when required hook fails")
    }
}

func TestLauncher_AfterHooksRun(t *testing.T) {
    configDir := t.TempDir()
    cwd := t.TempDir()
    writeMinimalConfig(t, configDir, `
agent: /usr/local/bin/my-agent
hooks:
  after:
    - name: cleanup
      run: "echo cleanup"
`)
    hookRunner := &mockHookRunner{}
    mock := &mockExecer{}
    l := &Launcher{
        Execer:     mock,
        ConfigDir:  configDir,
        HookRunner: hookRunner,
    }
    if err := l.Launch(cwd, "", nil, false, false); err != nil {
        t.Fatalf("Launch failed: %v", err)
    }
    if len(hookRunner.executed) != 1 || hookRunner.executed[0] != "cleanup" {
        t.Errorf("expected [cleanup], got %v", hookRunner.executed)
    }
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && go test ./internal/launcher/ -run "TestLauncher_BeforeHooks|TestLauncher_RequiredBeforeHook|TestLauncher_AfterHooks" -v`
Expected: FAIL — `HookRunner` field not on `Launcher`, hooks not wired

- [ ] **Step 3: Add HookRunner field and wire hooks into Launch()**

Add `HookRunner` field to `Launcher` struct in `launcher.go`:

```go
type Launcher struct {
    Execer     Execer
    HookRunner HookRunner   // nil → DefaultHookRunner
    ConfigDir  string
    LookPath   LookPathFunc
    Yolo       bool
    Stderr     io.Writer
}
```

Add a helper:

```go
func (l *Launcher) hookRunner() HookRunner {
    if l.HookRunner != nil {
        return l.HookRunner
    }
    return &DefaultHookRunner{}
}
```

In `Launch()`, after the banner (step 13) and before the exec (step 14), add:

```go
// 14. Run before-hooks
if rc.Context.Hooks != nil && len(rc.Context.Hooks.Before) > 0 {
    if err := RunBeforeHooks(rc.Context.Hooks.Before, l.hookRunner(), env, projectRoot); err != nil {
        cleanup()
        return err
    }
}

// 15. Run agent as subprocess
args := append([]string{binary}, extraArgs...)
exitCode, err := l.Execer.Run(binary, args, env)
if err != nil {
    // Best-effort after-hooks even on start failure
    if rc.Context.Hooks != nil {
        RunAfterHooks(rc.Context.Hooks.After, l.hookRunner(), env, projectRoot)
    }
    cleanup()
    return fmt.Errorf("running agent: %w", err)
}

// 16. Run after-hooks
if rc.Context.Hooks != nil && len(rc.Context.Hooks.After) > 0 {
    RunAfterHooks(rc.Context.Hooks.After, l.hookRunner(), env, projectRoot)
}

// 17. Cleanup and exit
cleanup()
return NewExitError(exitCode)
```

Also need to add the `config` import:

```go
import "github.com/jskswamy/aide/internal/config"
```

- [ ] **Step 4: Run the new tests**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && go test ./internal/launcher/ -run "TestLauncher_BeforeHooks|TestLauncher_RequiredBeforeHook|TestLauncher_AfterHooks" -v`
Expected: PASS

- [ ] **Step 5: Run ALL launcher tests**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && go test ./internal/launcher/ -v`
Expected: ALL PASS

- [ ] **Step 6: Commit**

```
git add internal/launcher/launcher.go internal/launcher/launcher_test.go
```
Message: `Wire lifecycle hooks into Launcher.Launch()`

---

### Task 8: Banner — Show Hooks Info

**Files:**
- Modify: `internal/ui/banner.go` (add hooks to BannerData)
- Modify: `internal/launcher/launcher.go` (populate hooks in banner data)

- [ ] **Step 1: Check current BannerData struct**

Read `internal/ui/banner.go` to understand the struct and rendering.

- [ ] **Step 2: Add HooksInfo to BannerData**

Add to the `BannerData` struct:

```go
type BannerData struct {
    // ... existing fields ...
    BeforeHookCount int
    AfterHookCount  int
}
```

- [ ] **Step 3: Add hooks line to banner rendering**

In the banner render function, after sandbox info, add:

```go
if data.BeforeHookCount > 0 || data.AfterHookCount > 0 {
    fmt.Fprintf(w, "  Hooks:   %d before, %d after\n", data.BeforeHookCount, data.AfterHookCount)
}
```

- [ ] **Step 4: Populate hooks counts in buildBannerData**

In `launcher.go`, in `buildBannerData()`, add:

```go
if rc.Context.Hooks != nil {
    data.BeforeHookCount = len(rc.Context.Hooks.Before)
    data.AfterHookCount = len(rc.Context.Hooks.After)
}
```

- [ ] **Step 5: Run tests**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && go test ./internal/... -v`
Expected: ALL PASS

- [ ] **Step 6: Commit**

```
git add internal/ui/banner.go internal/launcher/launcher.go
```
Message: `Show hooks count in startup banner`

---

### Task 9: Full Integration Test

**Files:**
- Create: `internal/launcher/integration_test.go`

- [ ] **Step 1: Write integration tests**

```go
// integration_test.go
//go:build integration

package launcher

import (
    "os"
    "path/filepath"
    "testing"
)

func TestIntegration_HooksRunInOrder(t *testing.T) {
    configDir := t.TempDir()
    cwd := t.TempDir()
    logFile := filepath.Join(cwd, "hook-log.txt")

    writeMinimalConfig(t, configDir, `
agent: /bin/sh
hooks:
  before:
    - name: step1
      run: "echo before1 >> `+logFile+`"
    - name: step2
      run: "echo before2 >> `+logFile+`"
  after:
    - name: step3
      run: "echo after1 >> `+logFile+`"
`)

    l := &Launcher{
        Execer:    &SubprocessExecer{},
        ConfigDir: configDir,
    }
    t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

    // Agent runs "echo agent" and exits
    err := l.Launch(cwd, "", []string{"-c", "echo agent >> " + logFile}, false, false)
    // May return ExitError(0) which is nil
    if err != nil {
        t.Fatalf("Launch failed: %v", err)
    }

    data, err := os.ReadFile(logFile)
    if err != nil {
        t.Fatalf("read log: %v", err)
    }
    expected := "before1\nbefore2\nagent\nafter1\n"
    if string(data) != expected {
        t.Errorf("hook execution order:\ngot:  %q\nwant: %q", string(data), expected)
    }
}

func TestIntegration_RequiredHookFailPreventsLaunch(t *testing.T) {
    configDir := t.TempDir()
    cwd := t.TempDir()
    marker := filepath.Join(cwd, "agent-ran.txt")

    writeMinimalConfig(t, configDir, `
agent: /bin/sh
hooks:
  before:
    - name: will-fail
      run: "exit 1"
`)

    l := &Launcher{
        Execer:    &SubprocessExecer{},
        ConfigDir: configDir,
    }
    t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

    err := l.Launch(cwd, "", []string{"-c", "touch " + marker}, false, false)
    if err == nil {
        t.Fatal("expected error from failing hook")
    }
    if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
        t.Error("agent should not have run when required hook fails")
    }
}
```

- [ ] **Step 2: Run integration tests**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && go test ./internal/launcher/ -tags=integration -run TestIntegration -v`
Expected: PASS

- [ ] **Step 3: Commit**

```
git add internal/launcher/integration_test.go
```
Message: `Add integration tests for supervisor lifecycle hooks`

---

### Note: Signal-Triggered After-Hook Timeout (Deferred)

The spec describes a collective 30-second timeout for after-hooks when triggered by a signal (vs per-hook 30s on normal exit). In this plan, signal handling works as follows: the supervisor forwards the signal to the child, the child exits, `Run()` returns, and then `Launch()` runs after-hooks in the normal flow. This covers the common case (SIGTERM/SIGINT where the child exits promptly). The collective timeout on signal cleanup is deferred to a follow-up task — it would require moving after-hook execution into the signal handler goroutine with a context deadline, which adds complexity without affecting the primary use case.

---

### Task 10: Final Verification

- [ ] **Step 1: Run all unit tests**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && go test ./... 2>&1 | tail -30`
Expected: ALL PASS

- [ ] **Step 2: Run go vet**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && go vet ./...`
Expected: No issues

- [ ] **Step 3: Build**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && make build`
Expected: Builds successfully

- [ ] **Step 4: Manual smoke test**

Run: `cd /Users/subramk/source/github.com/jskswamy/aide && ./bin/aide --resolve`
Verify: aide launches the agent as a subprocess (aide process stays alive as parent), banner shows correctly.
