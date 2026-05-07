//go:build !windows

package launcher

import (
	"strings"
	"testing"
	"time"
)

func TestDiagnoseExecer_CapturesExitCodeAndStderr(t *testing.T) {
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
	res, err := e.Run("/bin/sh", []string{"sh", "-c", "yes XXXXXXXX | head -c 4000 1>&2"}, nil)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if int64(len(res.StderrTail)) > 200 { // small slack: truncation marker is short
		t.Errorf("StderrTail not truncated: len=%d", len(res.StderrTail))
	}
	if res.StderrTruncatedBytes <= 0 {
		t.Errorf("StderrTruncatedBytes should be >0, got %d", res.StderrTruncatedBytes)
	}
}
