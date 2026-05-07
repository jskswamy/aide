//go:build !windows

package launcher

import "testing"

type fakeDiagRunner struct{ result *RunResult }

func (f *fakeDiagRunner) Run(string, []string, []string) (*RunResult, error) {
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
	if err := l.runDiagnose("/bin/echo", []string{"echo", "hi"}, nil, diagContext{}); err != nil {
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
	err := l.runDiagnose("/bin/false", []string{"false"}, nil, diagContext{})
	if err == nil {
		t.Fatal("expected non-nil error for non-zero exit code")
	}
	if ee, ok := err.(interface{ ExitCode() int }); !ok || ee.ExitCode() != 7 {
		t.Errorf("expected ExitCode()==7, got err=%v", err)
	}
}
