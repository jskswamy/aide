package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/testutil"
)

func worktreeWriteFixture(t *testing.T) (mainDir, wtDir string) {
	t.Helper()
	mainDir, wtDir = testutil.GitWorktree(t)
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	if err := os.MkdirAll(filepath.Join(configHome, "aide"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configHome, "aide", "config.yaml"),
		[]byte("profiles:\n  work:\n    agent: claude\ndefault_profile: work\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mainDir, ".aide.yaml"),
		[]byte("capabilities: [go]\nsandbox:\n  writable_extra: [build]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return mainDir, wtDir
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// In a linked worktree, project-scope edits write the worktree's own
// .aide.yaml (created if absent) and never copy inherited settings or
// rewritten paths into it; --main targets the main checkout's file.
func TestCapEnable_WorktreeWritesLocalFileByDefault(t *testing.T) {
	mainDir, wtDir := worktreeWriteFixture(t)
	t.Chdir(wtDir)

	var out bytes.Buffer
	cmd := capCmd()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"enable", "docker"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("cap enable: %v\n%s", err, out.String())
	}
	local := readFile(t, filepath.Join(wtDir, ".aide.yaml"))
	if !strings.Contains(local, "docker") || strings.Contains(local, "go") || strings.Contains(local, "build") {
		t.Errorf("worktree file should hold only its own edit, got:\n%s", local)
	}
	if main := readFile(t, filepath.Join(mainDir, ".aide.yaml")); strings.Contains(main, "docker") {
		t.Errorf("main checkout file changed:\n%s", main)
	}

	cmd = capCmd()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"enable", "python", "--main"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("cap enable --main: %v\n%s", err, out.String())
	}
	main := readFile(t, filepath.Join(mainDir, ".aide.yaml"))
	if !strings.Contains(main, "python") || !strings.Contains(main, "- build") || strings.Contains(main, mainDir) {
		t.Errorf("--main should edit the main file as written (relative paths kept), got:\n%s", main)
	}
}

func TestEnvSet_WorktreeWritesLocalFileAndMainOnRequest(t *testing.T) {
	mainDir, wtDir := worktreeWriteFixture(t)
	t.Chdir(wtDir)

	var out bytes.Buffer
	cmd := envCmd()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"set", "FOO", "local"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("env set: %v\n%s", err, out.String())
	}
	if local := readFile(t, filepath.Join(wtDir, ".aide.yaml")); !strings.Contains(local, "FOO: local") {
		t.Errorf("worktree file = %s", local)
	}

	cmd = envCmd()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"set", "BAR", "main", "--main"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("env set --main: %v\n%s", err, out.String())
	}
	if main := readFile(t, filepath.Join(mainDir, ".aide.yaml")); !strings.Contains(main, "BAR: main") {
		t.Errorf("main file = %s", main)
	}
}

// Editing a project file never bakes load-time absolute paths into it.
func TestCapEnable_KeepsRelativePathsAsWritten(t *testing.T) {
	mainDir, _ := worktreeWriteFixture(t)
	t.Chdir(mainDir)

	var out bytes.Buffer
	cmd := capCmd()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"enable", "docker"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("cap enable: %v\n%s", err, out.String())
	}
	main := readFile(t, filepath.Join(mainDir, ".aide.yaml"))
	if !strings.Contains(main, "- build") || strings.Contains(main, mainDir) {
		t.Errorf("relative path rewritten on save:\n%s", main)
	}
}

func TestMainFlagOutsideWorktreeFails(t *testing.T) {
	mainDir, _ := worktreeWriteFixture(t)
	t.Chdir(mainDir)
	cmd := capCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"enable", "docker", "--main"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "linked git worktree") {
		t.Errorf("err = %v, want a --main needs a linked worktree error", err)
	}
}
