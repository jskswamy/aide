package launcher

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"github.com/jskswamy/aide/internal/diag"
)

// diagContext bundles the launcher-derived inputs needed to populate
// a diag.PreInput. It is built inside Launch (where config, sandbox
// policy, and secret resolution are all in scope) and threaded through
// runDiagnose so the diagnose path stays a thin wrapper.
type diagContext struct {
	Version           string
	Commit            string
	BuildDate         string
	ResolvedConfig    string
	SecretSourcePaths []string
	AgeKeySource      string
	Sandbox           diag.SandboxInfo
}

// RunResult captures everything observable from a fork+exec child run.
type RunResult struct {
	ExitCode             int
	Signal               string // "SIGINT", "SIGTERM", ...; "" if not signal-killed
	Runtime              time.Duration
	StderrTail           string
	StderrTruncatedBytes int64
	Pid                  int // PID of the child process; populated after Start
}

// DiagnoseRunner is the narrow interface the diagnose path consumes.
// Lets tests inject fakes.
//
// Contract: Run returns exactly one of:
//   - (result, nil) when the child process completed (regardless of
//     exit code or signal); the result describes what happened.
//   - (nil, err) when the child could not be started or some
//     pre-Wait operation failed.
//
// Implementations must not return both a non-nil result and a non-nil
// error; callers may rely on this to simplify branching.
type DiagnoseRunner interface {
	Run(binary string, args []string, env []string) (*RunResult, error)
}

// exitError carries the child's exit code back to main so the process
// exits with the same code.
type exitError struct{ code int }

func (e *exitError) Error() string { return fmt.Sprintf("child exited with code %d", e.code) }
func (e *exitError) ExitCode() int { return e.code }

// envIntDefault reads a positive int from env or returns def.
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

// ShouldShowSignpost reports whether to nudge the user toward --diagnose
// after an abnormal child exit. Suppresses on clean exits and on
// user/system shutdown signals (SIGINT, SIGTERM, SIGHUP, SIGQUIT) since
// those represent intentional termination, not a failure worth diagnosing.
//
// Note: this predicate is currently unused in v1. The default exec path
// uses syscall.Exec, so aide is replaced by the child before it can
// observe the exit. Re-enabling the signpost becomes a one-line change
// once aide moves to fork+exec as the default execution strategy.
func ShouldShowSignpost(exitCode int, signal string) bool {
	if exitCode == 0 {
		return false
	}
	switch signal {
	case "SIGINT", "SIGTERM", "SIGHUP", "SIGQUIT":
		return false
	}
	switch exitCode {
	case 129, 130, 131, 143: // SIGHUP, SIGINT, SIGQUIT, SIGTERM as 128+signum
		return false
	}
	return true
}

// EmitSignpost writes the diagnostic hint to w. Caller is responsible
// for the predicate (see ShouldShowSignpost).
func EmitSignpost(w io.Writer) {
	_, _ = io.WriteString(w, "\nhint: re-run with 'aide --diagnose' to capture a diagnostic report.\n")
}
