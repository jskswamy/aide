// cmd/aide/match_rule_test.go
package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	if rule.Remote != "example.com/foo/bar" {
		t.Errorf("git repo with remote: got %+v, want Remote=example.com/foo/bar", rule)
	}
	if rule.Path != "" {
		t.Errorf("git repo with remote should not set Path: got %q", rule.Path)
	}
}

func TestAutoDetectMatchRule_RemoteWithCredentials_PersistsNormalizedForm(t *testing.T) {
	dir := t.TempDir()
	mustRun(t, dir, "git", "init")
	mustRun(t, dir, "git", "remote", "add", "origin", "https://user:s3cret@GitHub.com/org/repo.git")
	rule, desc := autoDetectMatchRule(dir)
	if rule.Remote != "github.com/org/repo" {
		t.Errorf("got %+v, want Remote=github.com/org/repo", rule)
	}
	if strings.Contains(desc, "s3cret") || strings.Contains(rule.Remote, "s3cret") {
		t.Errorf("credentials leaked: rule=%+v desc=%q", rule, desc)
	}
}

func TestAutoDetectMatchRule_LocalPathRemote_PathRule(t *testing.T) {
	dir := t.TempDir()
	mustRun(t, dir, "git", "init")
	mustRun(t, dir, "git", "remote", "add", "origin", "/srv/git/repo.git")
	rule, _ := autoDetectMatchRule(dir)
	if rule.Path != dir || rule.Remote != "" {
		t.Errorf("host-less remote: got %+v, want Path=%s", rule, dir)
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

func TestRulesCollide_RemoteVariants(t *testing.T) {
	cases := []struct {
		a, b      string
		wantMatch bool
	}{
		{"git@github.com:org/repo.git", "https://github.com/org/repo.git", true},
		{"git@github.com:org/repo.git", "ssh://git@github.com/org/repo.git", true},
		{"https://github.com/org/repo", "https://github.com/org/repo.git", true},
		{"git@github.com:org/repo.git", "git@github.com:org/other.git", false},
		{"git@github.com:org/repo.git", "git@gitlab.com:org/repo.git", false},
		{"github.com/org/repo", "git@GitHub.com:org/repo.git", true},
		{"github.com/org/repo", "github.com/org/other", false},
		{"/srv/git/a.git", "/srv/git/b.git", false},
		{"/srv/git/a.git", "/srv/git/a.git", true},
	}
	for _, tc := range cases {
		a := config.MatchRule{Remote: tc.a}
		b := config.MatchRule{Remote: tc.b}
		if got := rulesCollide(a, b); got != tc.wantMatch {
			t.Errorf("rulesCollide(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.wantMatch)
		}
	}
}

func TestRulesCollide_PathMatch(t *testing.T) {
	a := config.MatchRule{Path: "/home/user/project"}
	b := config.MatchRule{Path: "/home/user/project"}
	if !rulesCollide(a, b) {
		t.Error("same path should collide")
	}
	if rulesCollide(a, config.MatchRule{Path: "/home/user/other"}) {
		t.Error("different path should not collide")
	}
}

func TestRulesCollide_RemoteVsPath_NoCollision(t *testing.T) {
	a := config.MatchRule{Remote: "git@github.com:org/repo.git"}
	b := config.MatchRule{Path: "/home/user/project"}
	if rulesCollide(a, b) {
		t.Error("remote vs path should never collide")
	}
}

func TestFindExistingBind_DetectsCrossContextCollision(t *testing.T) {
	cfg := &config.Config{
		Contexts: map[string]config.Context{
			"work": {Match: []config.MatchRule{{Remote: "https://github.com/org/repo.git"}}},
		},
	}
	newRule := config.MatchRule{Remote: "git@github.com:org/repo.git"}
	got := findExistingBind(cfg, "personal", newRule)
	if got == nil || got.ctxName != "work" {
		t.Errorf("expected collision in 'work', got %v", got)
	}
}

func TestFindExistingBind_SkipsTargetContext(t *testing.T) {
	cfg := &config.Config{
		Contexts: map[string]config.Context{
			"work": {Match: []config.MatchRule{{Remote: "https://github.com/org/repo.git"}}},
		},
	}
	newRule := config.MatchRule{Remote: "git@github.com:org/repo.git"}
	got := findExistingBind(cfg, "work", newRule)
	if got != nil {
		t.Errorf("expected nil when collision is in skip context, got %+v", got)
	}
}

func TestRemoveMatchRule_RemovesByNormalizedRemote(t *testing.T) {
	rules := []config.MatchRule{
		{Remote: "https://github.com/org/repo.git"},
		{Path: "/keep/this"},
	}
	// Remove by the ssh variant — should still match via ParseRemoteHost
	result := removeMatchRule(rules, config.MatchRule{Remote: "git@github.com:org/repo.git"})
	if len(result) != 1 || result[0].Path != "/keep/this" {
		t.Errorf("expected only path rule to remain, got %+v", result)
	}
}

// Unused-import shield: keep filepath/config referenced even if a future test
// shrinks; harmless during plan execution.
var _ = filepath.Join
