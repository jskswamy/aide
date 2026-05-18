package provision

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
)

// Runner abstracts subprocess execution so drivers can be tested
// without the real agent binary. Production code uses ExecRunner.
type Runner interface {
	// Run executes name with args. env is merged on top of the parent
	// process environment — values inherit unless overridden by env
	// per-key. Returns stdout, stderr, the process exit code, and an
	// error. The error is non-nil for any failure to start, kill, or
	// wait on the process; a non-zero exit code is reported via
	// exitCode but does NOT by itself cause err to be non-nil
	// (drivers decide whether non-zero is an error in their context).
	Run(ctx context.Context, env map[string]string, name string, args ...string) (stdout string, stderr string, exitCode int, err error)
}

// ExecRunner runs subprocesses via os/exec. The zero value is usable.
type ExecRunner struct{}

// Run implements Runner with os/exec.CommandContext.
func (ExecRunner) Run(ctx context.Context, env map[string]string, name string, args ...string) (string, string, int, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = mergeEnv(os.Environ(), env)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()
	exit := 0
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			exit = ee.ExitCode()
			err = nil // non-zero exit is not a Run error
		}
	}
	return outBuf.String(), errBuf.String(), exit, err
}

// mergeEnv returns parent with override entries appended; child
// processes read the last-set value for a given key per POSIX, so
// later entries win.
func mergeEnv(parent []string, override map[string]string) []string {
	if len(override) == 0 {
		return parent
	}
	out := make([]string, 0, len(parent)+len(override))
	out = append(out, parent...)
	for k, v := range override {
		out = append(out, k+"="+v)
	}
	return out
}
