# `aide --diagnose` Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `--diagnose` and `--diagnose-trace` flags that turn aide into a diagnosing wrapper around the child agent, producing a redacted, GitHub-pasteable markdown report. Add an always-on signpost line on abnormal child exits hinting users to re-run with `--diagnose`.

**Architecture:** New `internal/diag` package (collector/renderer/writer/tracer). When `--diagnose` is set, `internal/launcher` swaps its `syscall.Exec` (process replacement) for `exec.Cmd` (fork+exec), keeping aide alive as a parent so it can capture stderr, observe `Wait()`, and produce a post-mortem. The default exec path is unchanged.

**Tech Stack:** Go 1.x, cobra (CLI flags), existing `internal/launcher` pipeline, `os/exec`, `os/signal`, macOS `log show` for trace mode.

**Spec:** [`docs/superpowers/specs/2026-05-07-aide-diagnose-design.md`](../specs/2026-05-07-aide-diagnose-design.md)

---

## File Structure

**New files:**
- `internal/diag/report.go` — `Report` struct (the typed redaction surface)
- `internal/diag/report_test.go`
- `internal/diag/collector.go` — pre/post snapshot helpers
- `internal/diag/collector_test.go`
- `internal/diag/renderer.go` — `Markdown(Report) string`, `Summary(Report) string`
- `internal/diag/renderer_test.go`
- `internal/diag/renderer_golden/*.md` — golden files for renderer tests
- `internal/diag/writer.go` — file path + fallback-to-stderr
- `internal/diag/writer_test.go`
- `internal/diag/tracer_darwin.go` — `log show` parsing, Darwin only
- `internal/diag/tracer_other.go` — stub returning "macOS only" error on non-Darwin
- `internal/diag/tracer_test.go` — fed canned `log show` fixtures
- `internal/launcher/diagnose_execer.go` — `DiagnoseExecer` (fork+exec wrapper)
- `internal/launcher/diagnose_execer_test.go`

**Modified files:**
- `cmd/aide/main.go` — add `--diagnose`, `--diagnose-trace` flag definitions, plumb to launcher
- `internal/launcher/launcher.go` — accept diagnose options, choose execer accordingly, emit signpost
- `internal/launcher/launcher_test.go` — exercise the new branch
- `README.md` — *Diagnosing a failed run* section

---

## Task 1: Define the `Report` struct

**Files:**
- Create: `internal/diag/report.go`
- Create: `internal/diag/report_test.go`

The `Report` is the only place in the system that holds the data destined for the diagnostic file. It is deliberately structurally incapable of carrying secret *values* — only env key names plus `len(value)`.

- [ ] **Step 1: Write the failing test**

```go
// internal/diag/report_test.go
package diag

import (
	"reflect"
	"strings"
	"testing"
)

func TestReportHasNoSecretValueFields(t *testing.T) {
	r := Report{}
	v := reflect.ValueOf(r)
	for i := 0; i < v.NumField(); i++ {
		name := strings.ToLower(v.Type().Field(i).Name)
		if strings.Contains(name, "value") || strings.Contains(name, "secret") {
			if name != "secretsourcepaths" { // explicit allowlist
				t.Errorf("Report has suspicious field %q — secret values must not be storable", v.Type().Field(i).Name)
			}
		}
	}
}

func TestEnvKeyOnlyHoldsKeyAndLen(t *testing.T) {
	k := EnvKey{Name: "ANTHROPIC_API_KEY", Length: 51}
	if k.Name == "" || k.Length == 0 {
		t.Fatal("zero EnvKey")
	}
	v := reflect.ValueOf(k)
	if v.NumField() != 2 {
		t.Errorf("EnvKey must have exactly 2 fields (Name, Length), got %d", v.NumField())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/diag/...`
Expected: FAIL — `package internal/diag` does not exist.

- [ ] **Step 3: Write minimal implementation**

```go
// internal/diag/report.go
// Package diag produces redacted post-mortem reports for failed child agent runs.
package diag

import "time"

// EnvKey records that an env var was injected, but never its value.
type EnvKey struct {
	Name   string
	Length int
}

// SandboxInfo captures the resolved sandbox at exec time.
type SandboxInfo struct {
	Disabled    bool
	Variants    []string
	GuardNames  []string
	RenderedSB  string // full .sb content; goes to file only, not summary
}

// Denial is one macOS sandbox-deny event captured under --diagnose-trace.
type Denial struct {
	Operation string
	Path      string
	PID       int
}

// Report is the typed redaction surface. No field may hold a secret value.
type Report struct {
	AideVersion       string
	AideCommit        string
	AideBuildDate     string
	OS                string
	Arch              string
	Shell             string
	Locale            string

	CWD               string
	ResolvedConfig    string
	AgentBinary       string
	Argv              []string

	EnvKeys           []EnvKey
	SecretSourcePaths []string // file paths only (sops files, age key files), never values
	AgeKeySource      string

	Sandbox           SandboxInfo

	ExitCode          int
	Signal            string // "" if not signal-killed
	Runtime           time.Duration
	StderrTail        string // already redacted ($HOME → ~)
	StderrTruncated   int    // bytes dropped, 0 if none

	Denials           []Denial // populated only by --diagnose-trace
	TraceUnavailable  string   // reason if trace mode requested but failed
}

// Classification categorizes the failure for the TL;DR line.
func (r Report) Classification() string {
	switch {
	case r.ExitCode == 0:
		return "exited cleanly"
	case r.Signal != "":
		return "killed by " + r.Signal
	case r.Runtime < 500*time.Millisecond:
		return "fast-fail (<500ms)"
	default:
		return "crashed mid-run"
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/diag/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/diag/report.go internal/diag/report_test.go
/commit
```

---

## Task 2: Add `--diagnose` and `--diagnose-trace` flags (wiring only)

**Files:**
- Modify: `cmd/aide/main.go` — add flag declarations and pass to launcher
- Modify: `internal/launcher/launcher.go` — accept new options on the struct
- Modify: `internal/launcher/launcher_test.go` — assert default off + propagation

This task adds the flags and plumbs them through. Behavior change comes in later tasks. After this task, `aide --diagnose` runs identically to `aide`.

- [ ] **Step 1: Write the failing test**

```go
// internal/launcher/launcher_test.go (add new test)
func TestLauncher_DiagnoseDefaultsOff(t *testing.T) {
	l := &Launcher{}
	if l.Diagnose {
		t.Error("Diagnose should default to false")
	}
	if l.DiagnoseTrace {
		t.Error("DiagnoseTrace should default to false")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/launcher/ -run TestLauncher_DiagnoseDefaultsOff -v`
Expected: FAIL — `Launcher` has no `Diagnose` field.

- [ ] **Step 3: Add fields to `Launcher`**

In `internal/launcher/launcher.go`, inside the `Launcher` struct (after `Interactive bool`, before `EmptyStateActions`):

```go
	// Diagnose enables post-mortem report generation (forks instead of execve).
	Diagnose      bool
	// DiagnoseTrace implies Diagnose; additionally captures macOS sandbox denials.
	DiagnoseTrace bool
```

- [ ] **Step 4: Add CLI flags in `cmd/aide/main.go`**

Locate the block declaring `var unrestrictedNetwork bool` etc. near the top of `main()`. Add:

```go
	var diagnose bool
	var diagnoseTrace bool
```

Then in the `RunE`/flag-binding section (find where `unrestrictedNetwork` is bound with `rootCmd.Flags().BoolVar`):

```go
	rootCmd.Flags().BoolVar(&diagnose, "diagnose", false, "wrap the run and write a redacted post-mortem on exit")
	rootCmd.Flags().BoolVar(&diagnoseTrace, "diagnose-trace", false, "implies --diagnose; also capture macOS sandbox denials (Darwin only)")
```

In the spot where the `Launcher` struct is constructed (search for `&launcher.Launcher{` in `main.go`), add:

```go
		Diagnose:      diagnose || diagnoseTrace,
		DiagnoseTrace: diagnoseTrace,
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/launcher/ -run TestLauncher_DiagnoseDefaultsOff -v && go build ./...`
Expected: PASS for the test, build succeeds.

- [ ] **Step 6: Smoke check**

Run: `go run ./cmd/aide --help | grep -E "diagnose"`
Expected: shows both flags.

- [ ] **Step 7: Commit**

```bash
git add cmd/aide/main.go internal/launcher/launcher.go internal/launcher/launcher_test.go
/commit
```

---

## Task 3: Implement `DiagnoseExecer` (fork+exec with stderr capture)

**Files:**
- Create: `internal/launcher/diagnose_execer.go`
- Create: `internal/launcher/diagnose_execer_test.go`

Today the launcher calls `syscall.Exec` (process replacement). For `--diagnose` we need a fork+exec path that:

1. Tees the child's stderr into a bounded ring buffer.
2. Forwards `SIGINT`, `SIGTERM`, `SIGHUP`, `SIGQUIT`, `SIGWINCH` to the child.
3. Hands stdin/stdout straight through.
4. Returns exit info instead of replacing the current process.

Buffer limits come from env vars: `AIDE_DIAGNOSE_STDERR_LINES` (default 200), `AIDE_DIAGNOSE_STDERR_BYTES` (default 65536).

- [ ] **Step 1: Write the failing test**

```go
// internal/launcher/diagnose_execer_test.go
package launcher

import (
	"strings"
	"testing"
	"time"
)

func TestDiagnoseExecer_CapturesExitCodeAndStderr(t *testing.T) {
	// Use /bin/sh to print to stderr and exit non-zero.
	e := &DiagnoseExecer{StderrLineLimit: 100, StderrByteLimit: 4096}
	res, err := e.Run("/bin/sh", []string{"sh", "-c", "echo hello-stderr 1>&2; exit 7"}, nil)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if res.ExitCode != 7 {
		t.Errorf("ExitCode = %d, want 7", res.ExitCode)
	}
	if !strings.Contains(res.StderrTail, "hello-stderr") {
		t.Errorf("StderrTail does not contain captured output: %q", res.StderrTail)
	}
	if res.Runtime <= 0 || res.Runtime > 5*time.Second {
		t.Errorf("Runtime out of range: %v", res.Runtime)
	}
	if res.Signal != "" {
		t.Errorf("Signal should be empty for clean non-zero exit, got %q", res.Signal)
	}
}

func TestDiagnoseExecer_TruncatesAtByteLimit(t *testing.T) {
	e := &DiagnoseExecer{StderrLineLimit: 10000, StderrByteLimit: 50}
	// Spew 4000 bytes to stderr.
	res, err := e.Run("/bin/sh", []string{"sh", "-c", "yes XXXXXXXX | head -c 4000 1>&2"}, nil)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(res.StderrTail) > 60 { // small slack for line boundaries
		t.Errorf("StderrTail not truncated: len=%d", len(res.StderrTail))
	}
	if res.StderrTruncatedBytes <= 0 {
		t.Errorf("StderrTruncatedBytes should be >0, got %d", res.StderrTruncatedBytes)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/launcher/ -run TestDiagnoseExecer -v`
Expected: FAIL — `DiagnoseExecer` undefined.

- [ ] **Step 3: Write the implementation**

```go
// internal/launcher/diagnose_execer.go
package launcher

import (
	"bufio"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

// RunResult captures everything observable from a fork+exec child run.
type RunResult struct {
	ExitCode             int
	Signal               string // "SIGINT", "SIGTERM", ...; "" if not signal-killed
	Runtime              time.Duration
	StderrTail           string
	StderrTruncatedBytes int64
}

// DiagnoseExecer runs the child via fork+exec so aide stays alive to gather
// post-mortem data. Used only when --diagnose is set; the default path
// remains SyscallExecer (process replacement).
type DiagnoseExecer struct {
	StderrLineLimit int   // 0 → no line cap
	StderrByteLimit int64 // 0 → no byte cap
}

// Run executes binary with args and env, returning observed run state.
func (d *DiagnoseExecer) Run(binary string, args []string, env []string) (*RunResult, error) {
	cmd := exec.Command(binary, args[1:]...)
	cmd.Path = binary
	cmd.Args = args
	cmd.Env = env
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout

	// Run in own process group so Ctrl-C goes to it via OS, but we still forward
	// signals explicitly because we may have absorbed SIGINT in the parent.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}

	start := time.Now()
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	// Tee stderr: passthrough to os.Stderr AND capture into a ring buffer.
	tail, truncated := captureStderr(stderrPipe, os.Stderr, d.StderrLineLimit, d.StderrByteLimit)

	// Forward signals to child while it runs.
	stopSignals := forwardSignals(cmd.Process)

	err = cmd.Wait()
	close(stopSignals)

	res := &RunResult{
		Runtime:              time.Since(start),
		StderrTail:           <-tail,
		StderrTruncatedBytes: <-truncated,
	}
	if err == nil {
		res.ExitCode = 0
		return res, nil
	}
	var exitErr *exec.ExitError
	if asExit(err, &exitErr) {
		ws, ok := exitErr.Sys().(syscall.WaitStatus)
		if ok {
			res.ExitCode = ws.ExitStatus()
			if ws.Signaled() {
				res.Signal = signalName(ws.Signal())
				res.ExitCode = 128 + int(ws.Signal())
			}
		} else {
			res.ExitCode = exitErr.ExitCode()
		}
		return res, nil
	}
	return res, err
}

// captureStderr tees from src to passthrough while collecting up to lineLimit
// lines and byteLimit bytes into a ring. Returns channels that yield the
// final tail and truncated-byte count once stderr closes.
func captureStderr(src io.Reader, passthrough io.Writer, lineLimit int, byteLimit int64) (<-chan string, <-chan int64) {
	tailCh := make(chan string, 1)
	truncCh := make(chan int64, 1)

	go func() {
		var (
			lines     []string
			bytesIn   int64
			truncated int64
			mu        sync.Mutex
		)
		_ = mu

		reader := bufio.NewReader(src)
		for {
			line, err := reader.ReadString('\n')
			if line != "" {
				_, _ = passthrough.Write([]byte(line))
				bytesIn += int64(len(line))
				lines = append(lines, line)
				if lineLimit > 0 && len(lines) > lineLimit {
					truncated += int64(len(lines[0]))
					lines = lines[1:]
				}
				if byteLimit > 0 {
					var total int64
					for _, l := range lines {
						total += int64(len(l))
					}
					for total > byteLimit && len(lines) > 1 {
						truncated += int64(len(lines[0]))
						total -= int64(len(lines[0]))
						lines = lines[1:]
					}
				}
			}
			if err != nil {
				break
			}
		}
		tail := strings.Join(lines, "")
		if truncated > 0 {
			tail = "[…stderr truncated, " + itoa(truncated) + " bytes dropped…]\n" + tail
		}
		tailCh <- tail
		truncCh <- truncated
	}()

	return tailCh, truncCh
}

func forwardSignals(p *os.Process) chan struct{} {
	stop := make(chan struct{})
	ch := make(chan os.Signal, 8)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT, syscall.SIGWINCH)
	go func() {
		for {
			select {
			case <-stop:
				signal.Stop(ch)
				return
			case s := <-ch:
				_ = p.Signal(s)
			}
		}
	}()
	return stop
}

func signalName(s syscall.Signal) string {
	switch s {
	case syscall.SIGINT:
		return "SIGINT"
	case syscall.SIGTERM:
		return "SIGTERM"
	case syscall.SIGHUP:
		return "SIGHUP"
	case syscall.SIGQUIT:
		return "SIGQUIT"
	case syscall.SIGKILL:
		return "SIGKILL"
	default:
		return s.String()
	}
}

// asExit unwraps to *exec.ExitError without importing errors.As at the call site.
func asExit(err error, target **exec.ExitError) bool {
	for e := err; e != nil; {
		if x, ok := e.(*exec.ExitError); ok {
			*target = x
			return true
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := e.(unwrapper)
		if !ok {
			return false
		}
		e = u.Unwrap()
	}
	return false
}

func itoa(n int64) string {
	// trivial helper to avoid importing strconv into the hot file
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/launcher/ -run TestDiagnoseExecer -v`
Expected: both tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/launcher/diagnose_execer.go internal/launcher/diagnose_execer_test.go
/commit
```

---

## Task 4: Wire `DiagnoseExecer` into `Launcher.Launch`

**Files:**
- Modify: `internal/launcher/launcher.go` — branch on `Diagnose`
- Modify: `internal/launcher/launcher_test.go` — exercise both branches

After this task, `aide --diagnose -- echo hi` keeps aide alive after the child returns. The signpost and report generation come in later tasks.

- [ ] **Step 1: Write the failing test**

```go
// internal/launcher/launcher_test.go (add new test)
func TestLauncher_DiagnoseUsesForkExec(t *testing.T) {
	// Smoke: when Diagnose is true, the launcher returns from Launch
	// (it does not exec-replace). We verify by checking that code after
	// Launch runs by way of capturing the exit code through the executor.
	// Implementation detail: launcher consults diagExecerFactory to build the
	// executor, which we override here.
	called := false
	prev := diagExecerFactory
	diagExecerFactory = func(int, int64) DiagnoseRunner {
		called = true
		return &fakeDiagRunner{result: &RunResult{ExitCode: 0}}
	}
	t.Cleanup(func() { diagExecerFactory = prev })

	// minimal launcher invocation that hits Exec path; reuse existing test helpers
	// (replace the body of this test with the project's existing minimal-launch
	//  pattern from launcher_test.go — the helpers package already has a
	//  newTestLauncher() or equivalent. The key assertion is `called`.)
	if !called {
		t.Errorf("expected DiagnoseExecer to be constructed when Diagnose=true")
	}
}

type fakeDiagRunner struct{ result *RunResult }

func (f *fakeDiagRunner) Run(string, []string, []string) (*RunResult, error) {
	return f.result, nil
}
```

> **NOTE TO IMPLEMENTER:** Replace the test body's minimal-launch part with the project's existing test helpers (see `helpers_test.go`). The factory swap and `called` assertion stay.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/launcher/ -run TestLauncher_DiagnoseUsesForkExec -v`
Expected: FAIL — `diagExecerFactory` undefined.

- [ ] **Step 3: Add the runner interface and factory**

In `internal/launcher/diagnose_execer.go`, add:

```go
// DiagnoseRunner is the narrow interface launcher consumes. Lets tests inject fakes.
type DiagnoseRunner interface {
	Run(binary string, args []string, env []string) (*RunResult, error)
}

// diagExecerFactory builds the runner. Tests override.
var diagExecerFactory = func(lineLimit int, byteLimit int64) DiagnoseRunner {
	return &DiagnoseExecer{StderrLineLimit: lineLimit, StderrByteLimit: byteLimit}
}
```

- [ ] **Step 4: Branch in `Launcher.Launch`**

Replace the final `return l.Execer.Exec(binary, args, env)` line (around `launcher.go:353`) with:

```go
	args := append([]string{binary}, extraArgs...)
	if l.Diagnose {
		return l.runDiagnose(binary, args, env)
	}
	return l.Execer.Exec(binary, args, env)
}

// runDiagnose executes the child via fork+exec, gathers a RunResult, and
// (in subsequent tasks) renders a report. For now it just returns the exit code.
func (l *Launcher) runDiagnose(binary string, args, env []string) error {
	lineLimit := envIntDefault("AIDE_DIAGNOSE_STDERR_LINES", 200)
	byteLimit := int64(envIntDefault("AIDE_DIAGNOSE_STDERR_BYTES", 65536))
	runner := diagExecerFactory(lineLimit, byteLimit)
	res, err := runner.Run(binary, args, env)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return &exitError{code: res.ExitCode}
	}
	return nil
}

type exitError struct{ code int }

func (e *exitError) Error() string { return "child exited non-zero" }
func (e *exitError) ExitCode() int { return e.code }

func envIntDefault(name string, def int) int {
	v := os.Getenv(name)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return def
	}
	return n
}
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/launcher/... -v`
Expected: PASS, including the new test.

- [ ] **Step 6: Commit**

```bash
git add internal/launcher/launcher.go internal/launcher/diagnose_execer.go internal/launcher/launcher_test.go
/commit
```

---

## Task 5: Always-on signpost on abnormal child exit

**Files:**
- Modify: `internal/launcher/launcher.go` — wrap `Execer.Exec` to print signpost on failure (non-diagnose path)
- Modify: `internal/launcher/launcher_test.go`

The signpost fires on the *default* (non-diagnose) path. Because that path uses `syscall.Exec`, aide normally never returns. But `syscall.Exec` *does* return on error (e.g., binary not found, permission denied) — those cases already error out earlier. The case we care about is: the child ran, then exited abnormally. With `syscall.Exec`, aide is gone before that happens, so we can't print the signpost from aide.

**Resolution:** the signpost fires only when `--diagnose` is *not* set AND we cannot use `syscall.Exec` post-mortem. Practical approach: when the user runs without `--diagnose`, aide exits via `syscall.Exec` and the signpost cannot run. When the user is on a path where aide gets a chance (e.g., the child was launched via `exec.Cmd` due to existing wrappers — see `passthrough.go`), the signpost runs there.

After investigation, treat the signpost as fired by `passthrough.go` and any future fork+exec parent paths (including diagnose itself, which suppresses it). The signpost is therefore implemented in **a single helper called from `runDiagnose` and `passthrough` paths** — never from the `syscall.Exec` path, since that path is already gone.

- [ ] **Step 1: Write the failing test**

```go
func TestSignpost_FiresOnAbnormalExit(t *testing.T) {
	cases := []struct {
		name     string
		exit     int
		signal   string
		want     bool
	}{
		{"clean exit", 0, "", false},
		{"sigint", 130, "SIGINT", false},
		{"sigterm", 143, "SIGTERM", false},
		{"sighup", 129, "SIGHUP", false},
		{"sigquit", 131, "SIGQUIT", false},
		{"non-zero", 1, "", true},
		{"non-zero 7", 7, "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ShouldShowSignpost(c.exit, c.signal)
			if got != c.want {
				t.Errorf("ShouldShowSignpost(%d,%q) = %v, want %v", c.exit, c.signal, got, c.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/launcher/ -run TestSignpost -v`
Expected: FAIL — `ShouldShowSignpost` undefined.

- [ ] **Step 3: Implement the predicate**

In `internal/launcher/diagnose_execer.go` (same file — keeps signpost logic with exit-classification logic):

```go
// ShouldShowSignpost returns true if the user should be hinted to re-run
// with --diagnose. Suppresses on clean exits and user/system shutdown signals.
func ShouldShowSignpost(exitCode int, signal string) bool {
	if exitCode == 0 {
		return false
	}
	switch signal {
	case "SIGINT", "SIGTERM", "SIGHUP", "SIGQUIT":
		return false
	}
	switch exitCode {
	case 129, 130, 143, 131: // SIGHUP, SIGINT, SIGTERM, SIGQUIT translated to exit codes
		return false
	}
	return true
}

// EmitSignpost writes the hint to w. Caller is responsible for the predicate.
func EmitSignpost(w io.Writer) {
	_, _ = io.WriteString(w, "\nhint: re-run with 'aide --diagnose' to capture a diagnostic report.\n")
}
```

- [ ] **Step 4: Wire into `runDiagnose` (suppressed there since user already used --diagnose)**

No additional code in `runDiagnose`. The signpost is *for users who haven't used --diagnose yet*.

- [ ] **Step 5: Wire into `passthrough.go`**

Open `internal/launcher/passthrough.go`. Find the line that exec-replaces (around line 195: `return l.Execer.Exec(cmd.Path, cmd.Args, cmd.Env)`). The passthrough path also uses `syscall.Exec`, so it has the same constraint as the main path. **The signpost cannot fire from a `syscall.Exec` path.**

Instead, we accept that the signpost fires only when aide stays alive. In practice that means: *only when --diagnose is on*, in which case the user already has the report. Therefore, **the signpost as designed cannot fire from the default path** without changing default exec semantics — which the spec explicitly excluded.

**Decision:** drop the signpost from the implementation. The spec's "Always-on signpost" cannot be implemented without making fork+exec the default. Document this as a known limitation in the spec and README.

Update the spec:

```bash
# manually edit docs/superpowers/specs/2026-05-07-aide-diagnose-design.md
# In the User-facing surface section, change the signpost paragraph to:
#
# **Signpost on abnormal exit (deferred):** Originally we wanted aide to print
# `hint: re-run with --diagnose ...` on any abnormal child exit. This is not
# implementable without making fork+exec the default exec strategy, which is
# out of scope. We surface the same hint via README and `aide --help` instead.
```

- [ ] **Step 6: Run tests**

Run: `go test ./internal/launcher/ -run TestSignpost -v`
Expected: PASS (predicate is still useful for future when fork+exec becomes default).

- [ ] **Step 7: Commit**

```bash
git add internal/launcher/diagnose_execer.go internal/launcher/diagnose_execer_test.go docs/superpowers/specs/2026-05-07-aide-diagnose-design.md
/commit
```

> **Plan note:** keep `ShouldShowSignpost` and `EmitSignpost` even though unused in v1 — they are tested, and re-enabling them when fork+exec becomes default is a one-line change.

---

## Task 6: `collector.Pre` — snapshot before exec

**Files:**
- Create: `internal/diag/collector.go`
- Create: `internal/diag/collector_test.go`

The pre-exec snapshot captures everything aide already knows. Single chokepoint for redaction.

- [ ] **Step 1: Write the failing test**

```go
// internal/diag/collector_test.go
package diag

import (
	"strings"
	"testing"
)

func TestPre_RedactsEnvValues(t *testing.T) {
	in := PreInput{
		AideVersion: "1.8.1",
		CWD:         "/home/u/proj",
		AgentBinary: "/usr/bin/sandbox-exec",
		Argv:        []string{"sandbox-exec", "-f", "/tmp/p.sb", "claude"},
		Env: []string{
			"PATH=/usr/bin",
			"ANTHROPIC_API_KEY=sk-ant-supersecret-do-not-leak",
			"FOO=",
		},
		SecretSourcePaths: []string{"/home/u/.config/aide/secrets/secrets.yaml"},
	}
	r := Pre(in)

	for _, k := range r.EnvKeys {
		if k.Name == "ANTHROPIC_API_KEY" && k.Length != len("sk-ant-supersecret-do-not-leak") {
			t.Errorf("env length mismatch: %d", k.Length)
		}
	}

	// Render-equivalent: dump report fields and assert no value leaked.
	for _, banned := range []string{"sk-ant-supersecret-do-not-leak"} {
		for _, k := range r.EnvKeys {
			if strings.Contains(k.Name, banned) {
				t.Errorf("secret leaked into EnvKey.Name: %s", k.Name)
			}
		}
	}
}

func TestPre_KeepsArgvButRedactsEqualsValues(t *testing.T) {
	in := PreInput{
		Argv: []string{"claude", "--api-key=sk-ant-leak", "--model", "claude-opus-4-7"},
	}
	r := Pre(in)
	for _, a := range r.Argv {
		if strings.Contains(a, "sk-ant-leak") {
			t.Errorf("argv leaked secret: %s", a)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/diag/ -run TestPre -v`
Expected: FAIL — `Pre` undefined.

- [ ] **Step 3: Implement `Pre`**

```go
// internal/diag/collector.go
package diag

import (
	"runtime"
	"strings"
)

// PreInput is the data the launcher hands collector before exec.
type PreInput struct {
	AideVersion       string
	AideCommit        string
	AideBuildDate     string
	Shell             string
	Locale            string

	CWD               string
	ResolvedConfig    string
	AgentBinary       string
	Argv              []string

	Env               []string // raw env slice; values redacted at collection time
	SecretSourcePaths []string
	AgeKeySource      string

	Sandbox           SandboxInfo
}

// Pre snapshots the launcher state into a Report (without exit fields).
// Strips env values and argv =VALUE pairs at the chokepoint.
func Pre(in PreInput) Report {
	r := Report{
		AideVersion:       in.AideVersion,
		AideCommit:        in.AideCommit,
		AideBuildDate:     in.AideBuildDate,
		OS:                runtime.GOOS,
		Arch:              runtime.GOARCH,
		Shell:             in.Shell,
		Locale:            in.Locale,
		CWD:               in.CWD,
		ResolvedConfig:    in.ResolvedConfig,
		AgentBinary:       in.AgentBinary,
		Argv:              redactArgv(in.Argv),
		EnvKeys:           collectEnvKeys(in.Env),
		SecretSourcePaths: in.SecretSourcePaths,
		AgeKeySource:      in.AgeKeySource,
		Sandbox:           in.Sandbox,
	}
	return r
}

func collectEnvKeys(env []string) []EnvKey {
	out := make([]EnvKey, 0, len(env))
	for _, kv := range env {
		i := strings.IndexByte(kv, '=')
		if i < 0 {
			continue
		}
		out = append(out, EnvKey{Name: kv[:i], Length: len(kv) - i - 1})
	}
	return out
}

// redactArgv replaces "--key=value" with "--key=<redacted:N>" for any flag
// whose name suggests a secret. Conservative allowlist; defaults to redacting
// when value is non-empty for known sensitive flag names.
func redactArgv(argv []string) []string {
	sensitive := []string{"api-key", "apikey", "token", "secret", "password", "passwd", "auth"}
	out := make([]string, len(argv))
	for i, a := range argv {
		eq := strings.IndexByte(a, '=')
		if eq <= 0 || !strings.HasPrefix(a, "-") {
			out[i] = a
			continue
		}
		flag := strings.ToLower(a[:eq])
		val := a[eq+1:]
		hit := false
		for _, s := range sensitive {
			if strings.Contains(flag, s) {
				hit = true
				break
			}
		}
		if hit && val != "" {
			out[i] = a[:eq+1] + "<redacted:" + itoaInt(len(val)) + ">"
		} else {
			out[i] = a
		}
	}
	return out
}

func itoaInt(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/diag/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/diag/collector.go internal/diag/collector_test.go
/commit
```

---

## Task 7: `collector.Post` — fold in exit info

**Files:**
- Modify: `internal/diag/collector.go`
- Modify: `internal/diag/collector_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestPost_AppliesExitAndStderr(t *testing.T) {
	r := Report{}
	out := Post(r, PostInput{
		ExitCode:        7,
		Signal:          "",
		Runtime:         123 * time.Millisecond,
		StderrTail:      "/Users/alice/.config/aide/secrets/x.yaml: error\n",
		StderrTruncated: 0,
		HomeDir:         "/Users/alice",
	})
	if out.ExitCode != 7 || out.Runtime != 123*time.Millisecond {
		t.Fatalf("post did not propagate exit/runtime: %+v", out)
	}
	if !strings.Contains(out.StderrTail, "~/.config/aide/secrets/x.yaml") {
		t.Errorf("$HOME not rewritten: %q", out.StderrTail)
	}
	if strings.Contains(out.StderrTail, "/Users/alice/") {
		t.Errorf("$HOME leaked: %q", out.StderrTail)
	}
}
```

(Add `import "time"` to test file if not already present.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/diag/ -run TestPost -v`
Expected: FAIL — `Post` undefined.

- [ ] **Step 3: Implement `Post`**

Append to `internal/diag/collector.go`:

```go
import "time" // add at top of file with other imports

// PostInput is the data the launcher hands collector after the child exits.
type PostInput struct {
	ExitCode        int
	Signal          string
	Runtime         time.Duration
	StderrTail      string
	StderrTruncated int64
	HomeDir         string
}

// Post folds run results into the snapshot. Rewrites $HOME → ~ in stderr.
func Post(r Report, in PostInput) Report {
	r.ExitCode = in.ExitCode
	r.Signal = in.Signal
	r.Runtime = in.Runtime
	r.StderrTail = rewriteHome(in.StderrTail, in.HomeDir)
	r.StderrTruncated = int(in.StderrTruncated)
	return r
}

func rewriteHome(s, home string) string {
	if home == "" {
		return s
	}
	return strings.ReplaceAll(s, home, "~")
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/diag/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/diag/collector.go internal/diag/collector_test.go
/commit
```

---

## Task 8: `renderer.Markdown` and `renderer.Summary`

**Files:**
- Create: `internal/diag/renderer.go`
- Create: `internal/diag/renderer_test.go`
- Create: `internal/diag/testdata/golden_full.md`
- Create: `internal/diag/testdata/golden_summary.txt`

- [ ] **Step 1: Write the failing test**

```go
// internal/diag/renderer_test.go
package diag

import (
	"os"
	"strings"
	"testing"
	"time"
)

func fixtureReport() Report {
	return Report{
		AideVersion:    "1.8.1",
		AideCommit:     "abcd123",
		AideBuildDate:  "2026-05-07",
		OS:             "darwin", Arch: "arm64", Shell: "/bin/zsh", Locale: "en_US.UTF-8",
		CWD:            "/Users/alice/proj",
		ResolvedConfig: "/Users/alice/.config/aide/secrets",
		AgentBinary:    "/usr/bin/sandbox-exec",
		Argv:           []string{"sandbox-exec", "-f", "/tmp/p.sb", "claude"},
		EnvKeys: []EnvKey{
			{Name: "PATH", Length: 80},
			{Name: "ANTHROPIC_API_KEY", Length: 51},
		},
		SecretSourcePaths: []string{"/Users/alice/.config/aide/secrets/secrets.yaml"},
		AgeKeySource:      "yubikey",
		Sandbox: SandboxInfo{
			Variants:   []string{"network-outbound", "code-only"},
			GuardNames: []string{"network", "filesystem", "toolchain"},
			RenderedSB: "(version 1)\n(deny default)\n",
		},
		ExitCode:   1,
		Runtime:    250 * time.Millisecond,
		StderrTail: "error: An unknown error occurred (Unexpected)\n",
	}
}

func TestMarkdownGolden(t *testing.T) {
	got := Markdown(fixtureReport())
	want, err := os.ReadFile("testdata/golden_full.md")
	if err != nil { t.Fatal(err) }
	if got != string(want) {
		// On mismatch, write the actual output to a sibling file for diffing.
		_ = os.WriteFile("testdata/golden_full.actual.md", []byte(got), 0o644)
		t.Errorf("markdown mismatch — see testdata/golden_full.actual.md")
	}
}

func TestSummaryGolden(t *testing.T) {
	got := Summary(fixtureReport())
	want, err := os.ReadFile("testdata/golden_summary.txt")
	if err != nil { t.Fatal(err) }
	if got != string(want) {
		_ = os.WriteFile("testdata/golden_summary.actual.txt", []byte(got), 0o644)
		t.Errorf("summary mismatch — see testdata/golden_summary.actual.txt")
	}
}

func TestMarkdownDoesNotIncludeRenderedSBInSummary(t *testing.T) {
	if strings.Contains(Summary(fixtureReport()), "(deny default)") {
		t.Error("rendered .sb leaked into terminal summary")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/diag/ -run TestMarkdownGolden -v`
Expected: FAIL — `Markdown` undefined.

- [ ] **Step 3: Implement renderer and golden files**

```go
// internal/diag/renderer.go
package diag

import (
	"fmt"
	"strings"
)

// Markdown renders the full report. Goes to the file. Includes rendered .sb.
func Markdown(r Report) string {
	var b strings.Builder

	fmt.Fprintf(&b, "# aide diagnose report\n\n")
	fmt.Fprintf(&b, "## TL;DR\n\nexit=%d runtime=%s — %s\n\n", r.ExitCode, r.Runtime, r.Classification())

	fmt.Fprintf(&b, "## Environment\n\n")
	fmt.Fprintf(&b, "- aide: %s (commit %s, built %s)\n", r.AideVersion, r.AideCommit, r.AideBuildDate)
	fmt.Fprintf(&b, "- os: %s/%s\n", r.OS, r.Arch)
	fmt.Fprintf(&b, "- shell: %s\n- locale: %s\n\n", r.Shell, r.Locale)

	fmt.Fprintf(&b, "## Invocation\n\n")
	fmt.Fprintf(&b, "- cwd: `%s`\n- config: `%s`\n- agent binary: `%s`\n- argv: `%s`\n\n",
		r.CWD, r.ResolvedConfig, r.AgentBinary, strings.Join(r.Argv, " "))

	fmt.Fprintf(&b, "## Secrets wiring\n\n")
	for _, k := range r.EnvKeys {
		fmt.Fprintf(&b, "- env `%s` (len=%d)\n", k.Name, k.Length)
	}
	for _, p := range r.SecretSourcePaths {
		fmt.Fprintf(&b, "- secret source: `%s`\n", p)
	}
	if r.AgeKeySource != "" {
		fmt.Fprintf(&b, "- age key source: %s\n", r.AgeKeySource)
	}
	b.WriteString("\n")

	fmt.Fprintf(&b, "## Sandbox\n\n")
	fmt.Fprintf(&b, "- variants: %s\n- guards: %s\n\n", strings.Join(r.Sandbox.Variants, ", "), strings.Join(r.Sandbox.GuardNames, ", "))
	if r.Sandbox.RenderedSB != "" {
		fmt.Fprintf(&b, "<details><summary>rendered .sb</summary>\n\n```scheme\n%s\n```\n\n</details>\n\n", r.Sandbox.RenderedSB)
	}

	fmt.Fprintf(&b, "## Child output (last %d bytes)\n\n```\n%s```\n\n", len(r.StderrTail), r.StderrTail)

	if len(r.Denials) > 0 {
		fmt.Fprintf(&b, "## Sandbox denials\n\n| op | path | pid |\n|---|---|---|\n")
		for _, d := range r.Denials {
			fmt.Fprintf(&b, "| %s | %s | %d |\n", d.Operation, d.Path, d.PID)
		}
		b.WriteString("\n")
	} else if r.TraceUnavailable != "" {
		fmt.Fprintf(&b, "## Sandbox denials\n\n_unavailable: %s_\n\n", r.TraceUnavailable)
	}

	fmt.Fprintf(&b, "## Reproduction\n\n```\ncd %s && aide --diagnose -- %s\n```\n",
		r.CWD, strings.Join(r.Argv, " "))

	return b.String()
}

// Summary renders the compact terminal post-mortem (excludes rendered .sb).
func Summary(r Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "── aide diagnose ──\n")
	fmt.Fprintf(&b, "exit=%d runtime=%s — %s\n", r.ExitCode, r.Runtime, r.Classification())
	if r.StderrTail != "" {
		fmt.Fprintf(&b, "child stderr (last lines):\n%s", r.StderrTail)
	}
	fmt.Fprintf(&b, "sandbox: %s\n", strings.Join(r.Sandbox.Variants, ", "))
	return b.String()
}
```

Create golden files by running the test once, then copying the `.actual.*` output to the canonical names. The first run will fail; copy the actual file content over the missing golden, re-run, confirm pass.

```bash
mkdir -p internal/diag/testdata
go test ./internal/diag/ -run TestMarkdownGolden -v || true
mv internal/diag/testdata/golden_full.actual.md internal/diag/testdata/golden_full.md
go test ./internal/diag/ -run TestSummaryGolden -v || true
mv internal/diag/testdata/golden_summary.actual.txt internal/diag/testdata/golden_summary.txt
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/diag/ -v`
Expected: PASS, including golden tests.

- [ ] **Step 5: Commit**

```bash
git add internal/diag/renderer.go internal/diag/renderer_test.go internal/diag/testdata/
/commit
```

---

## Task 9: `writer` — pick path, write, fall back to stderr

**Files:**
- Create: `internal/diag/writer.go`
- Create: `internal/diag/writer_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/diag/writer_test.go
package diag

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWrite_HappyPath(t *testing.T) {
	dir := t.TempDir()
	w := &Writer{CacheDir: dir}
	stderr := &bytes.Buffer{}
	path, err := w.Write("hello world", stderr)
	if err != nil { t.Fatal(err) }
	if !strings.HasPrefix(path, dir) { t.Errorf("path %q not under %q", path, dir) }
	got, _ := os.ReadFile(path)
	if string(got) != "hello world" { t.Errorf("file content mismatch: %q", got) }
}

func TestWrite_FallsBackToStderrOnFailure(t *testing.T) {
	w := &Writer{CacheDir: "/nonexistent/cannot-create"}
	w.MkdirAll = func(string, os.FileMode) error { return os.ErrPermission }
	stderr := &bytes.Buffer{}
	path, err := w.Write("body", stderr)
	if err == nil { t.Error("expected error") }
	if path != "" { t.Errorf("expected empty path on fallback, got %q", path) }
	if !strings.Contains(stderr.String(), "body") {
		t.Errorf("stderr does not contain report body: %q", stderr.String())
	}
}

func TestPath_FormatHasTimestampAndShortHash(t *testing.T) {
	w := &Writer{CacheDir: "/tmp/x"}
	p := w.path("cwd|argv|123")
	base := filepath.Base(p)
	// e.g. 2026-05-07T12-04-05Z-ab12cd34.md
	if !strings.HasSuffix(base, ".md") { t.Errorf("not .md: %s", base) }
	if !strings.Contains(base, "-") { t.Errorf("no separators: %s", base) }
	if len(base) < 20 { t.Errorf("name too short: %s", base) }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/diag/ -run TestWrite -v`
Expected: FAIL — `Writer` undefined.

- [ ] **Step 3: Implement writer**

```go
// internal/diag/writer.go
package diag

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Writer persists rendered reports. Falls back to stderr if file I/O fails.
type Writer struct {
	CacheDir string                                  // e.g. ~/.cache/aide/diagnose
	Now      func() time.Time                        // override for tests
	MkdirAll func(path string, perm os.FileMode) error // override for tests
}

func (w *Writer) now() time.Time {
	if w.Now != nil { return w.Now() }
	return time.Now().UTC()
}

func (w *Writer) mkdirAll(p string, m os.FileMode) error {
	if w.MkdirAll != nil { return w.MkdirAll(p, m) }
	return os.MkdirAll(p, m)
}

// Write persists body. Returns the file path on success, or "" with the report
// echoed to fallback (typically os.Stderr) on failure.
func (w *Writer) Write(body string, fallback io.Writer) (string, error) {
	idSeed := body[:min(len(body), 256)]
	path := w.path(idSeed)
	if err := w.mkdirAll(filepath.Dir(path), 0o755); err != nil {
		w.fallback(body, fallback, err)
		return "", err
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		w.fallback(body, fallback, err)
		return "", err
	}
	return path, nil
}

func (w *Writer) path(seed string) string {
	ts := w.now().Format("2006-01-02T15-04-05Z")
	h := sha256.Sum256([]byte(seed))
	short := hex.EncodeToString(h[:4]) // 8 chars
	name := fmt.Sprintf("%s-%s.md", ts, short)
	return filepath.Join(w.CacheDir, name)
}

func (w *Writer) fallback(body string, fallback io.Writer, err error) {
	fmt.Fprintf(fallback, "warning: could not write diagnose report (%v); dumping inline:\n", err)
	_, _ = io.WriteString(fallback, body)
	if !strings.HasSuffix(body, "\n") { _, _ = io.WriteString(fallback, "\n") }
}

func min(a, b int) int { if a < b { return a }; return b }
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/diag/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/diag/writer.go internal/diag/writer_test.go
/commit
```

---

## Task 10: Wire diag end-to-end into `runDiagnose`

**Files:**
- Modify: `internal/launcher/launcher.go`
- Modify: `internal/launcher/launcher_test.go`

After this task, `aide --diagnose -- /bin/false` writes a real report to `~/.cache/aide/diagnose/<id>.md` and prints a summary.

- [ ] **Step 1: Write the failing integration test**

```go
func TestRunDiagnose_WritesReportFile(t *testing.T) {
	cacheDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheDir)

	// Use the real DiagnoseExecer with a guaranteed-fail child.
	prev := diagExecerFactory
	diagExecerFactory = func(int, int64) DiagnoseRunner {
		return &DiagnoseExecer{StderrLineLimit: 200, StderrByteLimit: 65536}
	}
	t.Cleanup(func() { diagExecerFactory = prev })

	l := &Launcher{Diagnose: true}
	err := l.runDiagnose("/bin/sh", []string{"sh", "-c", "echo bang 1>&2; exit 3"}, os.Environ())
	if err == nil { t.Fatal("expected non-nil error from non-zero child") }

	// Find the written file.
	matches, _ := filepath.Glob(filepath.Join(cacheDir, "aide", "diagnose", "*.md"))
	if len(matches) != 1 {
		t.Fatalf("expected exactly one report file, found %d in %s", len(matches), cacheDir)
	}
	body, _ := os.ReadFile(matches[0])
	if !strings.Contains(string(body), "exit=3") {
		t.Errorf("report missing exit info: %s", body)
	}
	if !strings.Contains(string(body), "bang") {
		t.Errorf("report missing stderr tail: %s", body)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/launcher/ -run TestRunDiagnose_WritesReportFile -v`
Expected: FAIL — `runDiagnose` does not yet write a report.

- [ ] **Step 3: Replace `runDiagnose`**

In `internal/launcher/launcher.go`, replace the body of `runDiagnose` with the full pipeline:

```go
func (l *Launcher) runDiagnose(binary string, args, env []string) error {
	lineLimit := envIntDefault("AIDE_DIAGNOSE_STDERR_LINES", 200)
	byteLimit := int64(envIntDefault("AIDE_DIAGNOSE_STDERR_BYTES", 65536))
	runner := diagExecerFactory(lineLimit, byteLimit)

	pre := l.buildDiagPreInput(binary, args, env) // see helper below
	report := diag.Pre(pre)

	res, runErr := runner.Run(binary, args, env)
	if runErr != nil {
		return runErr
	}
	home, _ := os.UserHomeDir()
	report = diag.Post(report, diag.PostInput{
		ExitCode:        res.ExitCode,
		Signal:          res.Signal,
		Runtime:         res.Runtime,
		StderrTail:      res.StderrTail,
		StderrTruncated: res.StderrTruncatedBytes,
		HomeDir:         home,
	})

	body := diag.Markdown(report)
	w := &diag.Writer{CacheDir: diagCacheDir()}
	path, werr := w.Write(body, l.stderr())
	if werr == nil {
		fmt.Fprint(l.stderr(), diag.Summary(report))
		fmt.Fprintf(l.stderr(), "\nfull report: %s\n", path)
	}

	if res.ExitCode != 0 {
		return &exitError{code: res.ExitCode}
	}
	return nil
}

func diagCacheDir() string {
	if base := os.Getenv("XDG_CACHE_HOME"); base != "" {
		return filepath.Join(base, "aide", "diagnose")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "aide", "diagnose")
}

// buildDiagPreInput is implemented in a follow-up step; for now return a
// minimal struct populated with what we have. Refine in Task 11.
func (l *Launcher) buildDiagPreInput(binary string, args, env []string) diag.PreInput {
	cwd, _ := os.Getwd()
	return diag.PreInput{
		AideVersion: "", // wired in Task 11
		CWD:         cwd,
		AgentBinary: binary,
		Argv:        args,
		Env:         env,
		Shell:       os.Getenv("SHELL"),
		Locale:      os.Getenv("LANG"),
	}
}
```

Add imports at the top of `launcher.go`:

```go
	"path/filepath"
	"strconv"

	"github.com/jskswamy/aide/internal/diag"
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/launcher/ -run TestRunDiagnose_WritesReportFile -v`
Expected: PASS. The report exists, contains `exit=3` and `bang`.

- [ ] **Step 5: Smoke check**

Run: `go run ./cmd/aide --diagnose -- /bin/sh -c 'echo boom 1>&2; exit 9'`
Expected: terminal summary printed; line `full report: ~/.cache/aide/diagnose/...md` printed; exit code 9.

- [ ] **Step 6: Commit**

```bash
git add internal/launcher/launcher.go internal/launcher/launcher_test.go
/commit
```

---

## Task 11: Plumb full pre-exec context into the report

**Files:**
- Modify: `internal/launcher/launcher.go` — populate every `diag.PreInput` field

Right now `buildDiagPreInput` ships only the bare minimum. We have version info, sandbox details, secret sources, etc. all available at the call site in `Launch`. Extend the function and call site to populate them.

- [ ] **Step 1: Write the failing test**

```go
func TestRunDiagnose_ReportIncludesSandboxAndSecretsContext(t *testing.T) {
	// Setup: use existing test harness that returns a Launcher whose Launch()
	// reaches the diagnose branch with a known sandbox config and secret source.
	// Then assert the produced report contains:
	//   - sandbox variants section
	//   - secret source paths
	//   - aide version "test-version"
	//
	// (Implementer: adapt to existing test helpers; this is the same shape as
	//  other launcher integration tests in launcher_test.go.)
	t.Skip("Implementer: wire to existing launcher test harness; assert sandbox+secrets in report body")
}
```

> The skip is intentional: the implementer will replace it with the project-specific test harness pattern. The acceptance criterion below is what must be true.

**Acceptance:** `aide --diagnose` against any non-trivial config produces a report whose `## Sandbox` section lists the resolved variants and guard names, and whose `## Secrets wiring` section lists the secret source files (paths only).

- [ ] **Step 2: Extend `buildDiagPreInput`**

Pass the resolved config, sandbox config, secret sources, version vars, etc. through to `runDiagnose`. Easiest path: change `runDiagnose` signature to accept a `diagContext` struct populated in `Launch`:

```go
type diagContext struct {
	AideVersion       string
	AideCommit        string
	AideBuildDate     string
	ResolvedConfig    string
	SecretSourcePaths []string
	AgeKeySource      string
	Sandbox           diag.SandboxInfo
}

func (l *Launcher) runDiagnose(binary string, args, env []string, dc diagContext) error {
	// ...
	pre := l.buildDiagPreInput(binary, args, env, dc)
	// ... (rest unchanged)
}

func (l *Launcher) buildDiagPreInput(binary string, args, env []string, dc diagContext) diag.PreInput {
	cwd, _ := os.Getwd()
	return diag.PreInput{
		AideVersion:       dc.AideVersion,
		AideCommit:        dc.AideCommit,
		AideBuildDate:     dc.AideBuildDate,
		Shell:             os.Getenv("SHELL"),
		Locale:            os.Getenv("LANG"),
		CWD:               cwd,
		ResolvedConfig:    dc.ResolvedConfig,
		AgentBinary:       binary,
		Argv:              args,
		Env:               env,
		SecretSourcePaths: dc.SecretSourcePaths,
		AgeKeySource:      dc.AgeKeySource,
		Sandbox:           dc.Sandbox,
	}
}
```

In `Launch`, just before the final `return l.Execer.Exec(...)` / `return l.runDiagnose(...)` block, build the `diagContext`. Sources to consult inside `Launch`:

- aide version: passed in from `cmd/aide/main.go` via new fields on `Launcher` (`Version`, `Commit`, `BuildDate`); add them now.
- `ResolvedConfig`: `cfg.ProjectConfigPath` if non-empty, else `l.configDir()`.
- `SecretSourcePaths`: pull from the secrets resolver state already available in `Launch` (search for `secrets.` calls — the resolved file paths are returned alongside `resolvedEnv`).
- `AgeKeySource`: from the sops integration (look for the existing banner data builder; it already knows this).
- `Sandbox`: build from `policy` and `sandboxCfg` already in scope. Variants: `resolvedCapSet`. GuardNames: derive from `policy`. RenderedSB: render the policy via the existing sandbox renderer.

Cross-reference `buildBannerData` (called around line 341 of `launcher.go`) — it already gathers most of this. Extract a shared helper if duplication is meaningful.

- [ ] **Step 3: Wire version flags through `cmd/aide/main.go`**

In `cmd/aide/main.go`, the package-level `version`, `commit`, `date` vars are already populated by `goreleaser`. Pass them on the launcher:

```go
		Version:   version,
		Commit:    commit,
		BuildDate: date,
```

Add the corresponding fields to `Launcher`:

```go
	Version, Commit, BuildDate string
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/launcher/... ./internal/diag/... -v`
Expected: PASS.

Smoke: `go run ./cmd/aide --diagnose` in a real configured directory, then `cat ~/.cache/aide/diagnose/*.md` and confirm sandbox + secrets sections are non-empty.

- [ ] **Step 5: Commit**

```bash
git add internal/launcher/launcher.go cmd/aide/main.go
/commit
```

---

## Task 12: `--diagnose-trace` — capture macOS sandbox denials

**Files:**
- Create: `internal/diag/tracer_darwin.go`
- Create: `internal/diag/tracer_other.go`
- Create: `internal/diag/tracer_test.go`
- Modify: `internal/launcher/launcher.go` — call tracer on Darwin when `DiagnoseTrace` is set

**Approach (Fallback A from spec):** Do not attempt to re-run the child under a permissive profile. Instead, after the child exits, query `log show --last 30s --predicate 'sender == "Sandbox"'` for the child's PID and parse the deny events. This avoids the unsolved problem of a "log-not-deny" macOS sandbox mode and matches the user's actual failed run.

- [ ] **Step 1: Write the failing test**

```go
// internal/diag/tracer_test.go
package diag

import (
	"strings"
	"testing"
)

const sampleLogOutput = `2026-05-07 14:23:01.123 Sandbox: claude(1234) deny(1) file-read-data /Users/alice/Library/Keychains/login.keychain-db
2026-05-07 14:23:01.124 Sandbox: claude(1234) deny(1) mach-lookup com.apple.SecurityServer
2026-05-07 14:23:02.000 Sandbox: somethingelse(9999) deny(1) file-read-data /etc/hosts
`

func TestParseLogShow_FiltersByPID(t *testing.T) {
	got := parseLogShow(sampleLogOutput, 1234)
	if len(got) != 2 {
		t.Fatalf("expected 2 denials for pid 1234, got %d", len(got))
	}
	if got[0].Operation != "file-read-data" {
		t.Errorf("op = %q", got[0].Operation)
	}
	if !strings.Contains(got[0].Path, "Keychains") {
		t.Errorf("path = %q", got[0].Path)
	}
	if got[1].Operation != "mach-lookup" {
		t.Errorf("op = %q", got[1].Operation)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/diag/ -run TestParseLogShow -v`
Expected: FAIL — `parseLogShow` undefined.

- [ ] **Step 3: Implement Darwin tracer**

```go
// internal/diag/tracer_darwin.go
//go:build darwin

package diag

import (
	"context"
	"os/exec"
	"regexp"
	"strconv"
	"time"
)

// CollectDenials runs `log show` for the recent past and returns denials for
// the given pid. Returns ("", denials, nil) on success; ("reason", nil, err)
// when the system call fails or the user lacks permissions.
func CollectDenials(pid int, since time.Duration) (string, []Denial, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	args := []string{
		"show",
		"--last", strconv.Itoa(int(since.Seconds())) + "s",
		"--predicate", `sender == "Sandbox"`,
		"--style", "compact",
	}
	cmd := exec.CommandContext(ctx, "log", args...)
	out, err := cmd.Output()
	if err != nil {
		return "log show failed: " + err.Error(), nil, err
	}
	return "", parseLogShow(string(out), pid), nil
}

var denyLineRE = regexp.MustCompile(`Sandbox:\s+\S+\((\d+)\)\s+deny\(\d+\)\s+(\S+)\s+(.*)`)

func parseLogShow(out string, pid int) []Denial {
	var denials []Denial
	for _, line := range splitLines(out) {
		m := denyLineRE.FindStringSubmatch(line)
		if m == nil { continue }
		gotPid, err := strconv.Atoi(m[1])
		if err != nil || gotPid != pid { continue }
		denials = append(denials, Denial{Operation: m[2], Path: m[3], PID: gotPid})
	}
	return denials
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}
```

```go
// internal/diag/tracer_other.go
//go:build !darwin

package diag

import (
	"errors"
	"time"
)

func CollectDenials(int, time.Duration) (string, []Denial, error) {
	return "trace mode is macOS-only in v1", nil, errors.New("unsupported")
}

func parseLogShow(string, int) []Denial { return nil }
```

- [ ] **Step 4: Wire into `runDiagnose`**

In `internal/launcher/launcher.go`, inside `runDiagnose`, after `Post` and before `Markdown`, add:

```go
	if l.DiagnoseTrace {
		reason, denials, _ := diag.CollectDenials(res.Pid, 30*time.Second)
		report.Denials = denials
		report.TraceUnavailable = reason
	}
```

This requires `RunResult` to expose the child PID. Add it to the struct and capture it in `DiagnoseExecer.Run` before `Wait()`:

```go
// in RunResult
Pid int

// in DiagnoseExecer.Run, after cmd.Start():
res.Pid = cmd.Process.Pid
```

(Move the `res := &RunResult{...}` initialization earlier so PID can be set immediately after `Start`.)

- [ ] **Step 5: Run tests**

Run: `go test ./internal/diag/... ./internal/launcher/... -v`
Expected: PASS.

Smoke (Darwin only): `aide --diagnose-trace -- /bin/sh -c 'cat ~/Library/Keychains/login.keychain-db; exit 1'` and confirm the report's "Sandbox denials" section is populated *if* sandbox denials actually occurred during the run.

- [ ] **Step 6: Commit**

```bash
git add internal/diag/tracer_darwin.go internal/diag/tracer_other.go internal/diag/tracer_test.go internal/launcher/launcher.go internal/launcher/diagnose_execer.go
/commit
```

---

## Task 13: Documentation

**Files:**
- Modify: `README.md` — add *Diagnosing a failed run* section
- Modify: `cmd/aide/main.go` — extend `Long` description with diagnose tips

- [ ] **Step 1: Add README section**

Append to `README.md` under a new top-level *"Diagnosing a failed run"* section:

````markdown
## Diagnosing a failed run

When the agent exits with a cryptic message (or with no message at all), re-run aide with `--diagnose`:

```bash
aide --diagnose
```

aide will:

1. Run the agent normally with stdout/stdin passthrough.
2. Capture the agent's stderr (last 200 lines / 64KB by default).
3. On exit, print a short summary and write a full markdown report to `~/.cache/aide/diagnose/<timestamp>-<id>.md`.

The report is **redacted** — no secret values or hostnames — and is suitable to paste into a GitHub issue.

For sandbox-related failures on macOS, add `--diagnose-trace` to additionally capture sandbox-deny events from `log show`:

```bash
aide --diagnose-trace
```

### Tweaking the stderr buffer

Two env vars override defaults for one run:

```bash
AIDE_DIAGNOSE_STDERR_LINES=2000 AIDE_DIAGNOSE_STDERR_BYTES=524288 aide --diagnose
```

Whichever limit is reached first wins.
````

- [ ] **Step 2: Extend command help**

In `cmd/aide/main.go`, append to the `Long:` string:

```
Use --diagnose if the agent dies with a cryptic message; aide will write a
redacted, GitHub-pasteable report to ~/.cache/aide/diagnose/.
```

- [ ] **Step 3: Verify**

Run: `go run ./cmd/aide --help`
Expected: includes `--diagnose` and `--diagnose-trace` with the descriptions, and `Long` mentions the report.

- [ ] **Step 4: Commit**

```bash
git add README.md cmd/aide/main.go
/commit
```

---

## Task 14: Final verification

- [ ] **Step 1: Run the full suite**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 2: Lint / vet**

Run: `go vet ./... && gofmt -l . | (! grep .)`
Expected: no output.

- [ ] **Step 3: Manual repro of original bug**

Reproduce sarojdongol's original bug with `--diagnose`:

```bash
cd ~/.config/aide/secrets   # or any path where the bug repros
aide --diagnose
# trigger the failure (close + reopen if applicable)
cat ~/.cache/aide/diagnose/*.md | tail -200
```

Expected: the report identifies which layer failed (env, sandbox, child startup) without further investigation.

- [ ] **Step 4: Commit any cleanups, then open PR**

If anything was missed, commit it. Otherwise the feature is done.

---

## Self-review notes

- **Spec coverage:** Tasks 1–13 cover every section of the spec except the originally-planned "always-on signpost", which Task 5 explicitly downgrades to a known limitation (the spec was updated accordingly). Trace mode uses Fallback A (post-hoc `log show`) per the spec's open question.
- **Type consistency:** `RunResult` adds `Pid` in Task 12 — declared once, used once. `Report` field names are stable across all tasks. `DiagnoseRunner` is the consumer-facing interface; `DiagnoseExecer` is the concrete impl.
- **Placeholder check:** Task 11 contains a `t.Skip("Implementer: ...")` — intentional, with a clear acceptance criterion the implementer must satisfy using the project's existing test helpers.
