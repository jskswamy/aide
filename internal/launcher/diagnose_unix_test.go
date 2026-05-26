//go:build !windows

package launcher

import (
	"os"
	"testing"
)

type fakeDiagRunner struct {
	result          *RunResult
	gotExtraFiles   []*os.File
	gotArgs         []string
}

func (f *fakeDiagRunner) Run(_ string, args []string, _ []string, extraFiles []*os.File) (*RunResult, error) {
	f.gotArgs = args
	f.gotExtraFiles = extraFiles
	return f.result, nil
}

func TestLauncher_DiagnoseUsesForkExec(t *testing.T) {
	called := false
	prev := diagExecerFactory
	diagExecerFactory = func(int, int64) DiagnoseRunner {
		called = true
		return &fakeDiagRunner{result: &RunResult{ExitCode: 0}}
	}
	t.Cleanup(func() { diagExecerFactory = prev })

	l := &Launcher{Diagnose: true}
	if err := l.runDiagnose("/bin/echo", []string{"echo", "hi"}, nil, nil, diagContext{}); err != nil {
		t.Fatalf("runDiagnose returned error: %v", err)
	}
	if !called {
		t.Errorf("expected diagExecerFactory to be invoked when Diagnose=true")
	}
}

func TestLauncher_DiagnoseReturnsExitError(t *testing.T) {
	prev := diagExecerFactory
	diagExecerFactory = func(int, int64) DiagnoseRunner {
		return &fakeDiagRunner{result: &RunResult{ExitCode: 7}}
	}
	t.Cleanup(func() { diagExecerFactory = prev })

	l := &Launcher{Diagnose: true}
	err := l.runDiagnose("/bin/false", []string{"false"}, nil, nil, diagContext{})
	if err == nil {
		t.Fatal("expected non-nil error for non-zero exit code")
	}
	if ee, ok := err.(interface{ ExitCode() int }); !ok || ee.ExitCode() != 7 {
		t.Errorf("expected ExitCode()==7, got err=%v", err)
	}
}

// TestLauncher_DiagnoseForwardsExtraFiles pins that runDiagnose threads
// ExtraFiles through to the DiagnoseRunner. Without this, the sandbox
// memfd parked by applyLandlock/applyBwrap gets closed by cmd.Start
// before the child reads it. Greptile P1 (both Landlock and bwrap-seccomp
// findings collapse to the same plumbing here).
func TestLauncher_DiagnoseForwardsExtraFiles(t *testing.T) {
	f, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("open /dev/null: %v", err)
	}
	defer func() { _ = f.Close() }()

	runner := &fakeDiagRunner{result: &RunResult{ExitCode: 0}}
	prev := diagExecerFactory
	diagExecerFactory = func(int, int64) DiagnoseRunner { return runner }
	t.Cleanup(func() { diagExecerFactory = prev })

	l := &Launcher{Diagnose: true}
	if err := l.runDiagnose("/bin/echo", []string{"echo", "hi"}, nil, []*os.File{f}, diagContext{}); err != nil {
		t.Fatalf("runDiagnose returned error: %v", err)
	}

	if len(runner.gotExtraFiles) != 1 || runner.gotExtraFiles[0] != f {
		t.Errorf("DiagnoseRunner.Run did not receive ExtraFiles slice; got %v", runner.gotExtraFiles)
	}
}
