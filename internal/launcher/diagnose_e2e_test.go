//go:build !windows

package launcher

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/diag"
)

func TestRunDiagnose_WritesReportFile(t *testing.T) {
	cacheDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheDir)

	prev := diagExecerFactory
	diagExecerFactory = func(int, int64) DiagnoseRunner {
		return &DiagnoseExecer{StderrLineLimit: 200, StderrByteLimit: 65536}
	}
	t.Cleanup(func() { diagExecerFactory = prev })

	var stderr bytes.Buffer
	l := &Launcher{Diagnose: true, Stderr: &stderr}
	err := l.runDiagnose("/bin/sh", []string{"sh", "-c", "echo bang 1>&2; exit 3"}, os.Environ(), diagContext{})
	if err == nil {
		t.Fatal("expected non-nil error from non-zero child")
	}

	matches, _ := filepath.Glob(filepath.Join(cacheDir, "aide", "diagnose", "*.md"))
	if len(matches) != 1 {
		t.Fatalf("expected exactly one report file, found %d in %s", len(matches), cacheDir)
	}
	body, _ := os.ReadFile(matches[0])
	bs := string(body)
	if !strings.Contains(bs, "exit=3") {
		t.Errorf("report missing exit info, body:\n%s", bs)
	}
	if !strings.Contains(bs, "bang") {
		t.Errorf("report missing stderr tail, body:\n%s", bs)
	}

	stderrOut := stderr.String()
	if !strings.Contains(stderrOut, "── aide diagnose ──") {
		t.Errorf("terminal summary missing from stderr: %q", stderrOut)
	}
	if !strings.Contains(stderrOut, "full report:") {
		t.Errorf("terminal output missing 'full report:' line, got: %q", stderrOut)
	}
}

func TestRunDiagnose_PopulatesContext(t *testing.T) {
	cacheDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheDir)

	prev := diagExecerFactory
	diagExecerFactory = func(int, int64) DiagnoseRunner {
		return &fakeDiagRunner{result: &RunResult{ExitCode: 0}}
	}
	t.Cleanup(func() { diagExecerFactory = prev })

	var stderr bytes.Buffer
	l := &Launcher{
		Diagnose:  true,
		Stderr:    &stderr,
		Version:   "test-version",
		Commit:    "test-commit",
		BuildDate: "test-date",
	}
	dc := diagContext{
		Version:           "test-version",
		Commit:            "test-commit",
		BuildDate:         "test-date",
		ResolvedConfig:    "/fake/config/dir",
		SecretSourcePaths: []string{"/fake/path/secrets.yaml"},
		AgeKeySource:      "file:/fake/keys.txt",
		Sandbox: diag.SandboxInfo{
			Variants:   []string{"test-variant"},
			GuardNames: []string{"test-guard"},
			RenderedSB: "(version 1)",
		},
	}
	if err := l.runDiagnose("/bin/echo", []string{"echo", "hi"}, nil, dc); err != nil {
		t.Fatalf("runDiagnose returned error: %v", err)
	}

	matches, _ := filepath.Glob(filepath.Join(cacheDir, "aide", "diagnose", "*.md"))
	if len(matches) != 1 {
		t.Fatalf("expected exactly one report file, found %d", len(matches))
	}
	body, _ := os.ReadFile(matches[0])
	bs := string(body)
	for _, want := range []string{
		"test-version",
		"test-commit",
		"test-date",
		"/fake/config/dir",
		"/fake/path/secrets.yaml",
		"file:/fake/keys.txt",
		"test-variant",
		"test-guard",
		"(version 1)",
	} {
		if !strings.Contains(bs, want) {
			t.Errorf("report missing %q\nbody:\n%s", want, bs)
		}
	}
}
