package provision_test

import (
	"context"
	"runtime"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/provision"
)

func TestExecRunnerSuccess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires POSIX /usr/bin/true")
	}
	r := provision.ExecRunner{}
	_, _, code, err := r.Run(context.Background(), nil, "/usr/bin/true")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if code != 0 {
		t.Errorf("exit = %d, want 0", code)
	}
}

func TestExecRunnerNonZeroExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires POSIX /usr/bin/false")
	}
	r := provision.ExecRunner{}
	_, _, code, err := r.Run(context.Background(), nil, "/usr/bin/false")
	if err != nil {
		t.Fatalf("err should be nil for non-zero exit, got %v", err)
	}
	if code == 0 {
		t.Errorf("exit = %d, want non-zero", code)
	}
}

func TestExecRunnerCapturesStdout(t *testing.T) {
	r := provision.ExecRunner{}
	out, _, code, err := r.Run(context.Background(), nil, "echo", "hello")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if code != 0 {
		t.Errorf("exit = %d", code)
	}
	if !strings.Contains(out, "hello") {
		t.Errorf("stdout = %q, want contains 'hello'", out)
	}
}

func TestExecRunnerMissingBinary(t *testing.T) {
	r := provision.ExecRunner{}
	_, _, _, err := r.Run(context.Background(), nil, "/no/such/binary-aide-test")
	if err == nil {
		t.Fatal("expected err for missing binary")
	}
}
