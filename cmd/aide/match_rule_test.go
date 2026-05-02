// cmd/aide/match_rule_test.go
package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/jskswamy/aide/internal/config"
)

func TestAutoDetectMatchRule_NonGitFolder_PathRule(t *testing.T) {
	dir := t.TempDir()
	rule, desc := autoDetectMatchRule(dir)
	if rule.Path != dir || rule.Remote != "" {
		t.Errorf("non-git folder: got %+v, want Path=%s", rule, dir)
	}
	if desc == "" {
		t.Error("description must be non-empty")
	}
}

func TestAutoDetectMatchRule_GitRepoWithRemote_RemoteRule(t *testing.T) {
	dir := t.TempDir()
	mustRun(t, dir, "git", "init")
	mustRun(t, dir, "git", "remote", "add", "origin", "git@example.com:foo/bar.git")
	rule, _ := autoDetectMatchRule(dir)
	if rule.Remote != "git@example.com:foo/bar.git" {
		t.Errorf("git repo with remote: got %+v, want Remote=git@example.com:foo/bar.git", rule)
	}
	if rule.Path != "" {
		t.Errorf("git repo with remote should not set Path: got %q", rule.Path)
	}
}

func TestAutoDetectMatchRule_GitRepoNoRemote_PathRule(t *testing.T) {
	dir := t.TempDir()
	mustRun(t, dir, "git", "init")
	rule, _ := autoDetectMatchRule(dir)
	if rule.Path != dir {
		t.Errorf("git repo no remote: got %+v, want Path=%s", rule, dir)
	}
}

func mustRun(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=t@t",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, string(out))
	}
}

// Unused-import shield: keep filepath/config referenced even if a future test
// shrinks; harmless during plan execution.
var _ = filepath.Join
var _ = config.MatchRule{}
