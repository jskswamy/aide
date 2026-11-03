// cmd/aide/context_bind_test.go
package main

import (
	"bytes"
	"github.com/jskswamy/aide/internal/testutil"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func runContextBind(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := contextBindCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

// isolatedConfigDir builds a tempdir with HOME/XDG redirected so the
// tests cannot read the developer's real config. Returns the dir.
func isolatedConfigDir(t *testing.T) string {
	t.Helper()
	dir := testutil.CanonicalTempDir(t)
	t.Chdir(dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg"))
	t.Setenv("HOME", dir)
	// An exported agent config dir would redirect provisioning away
	// from the temp HOME.
	unsetEnv(t, "CLAUDE_CONFIG_DIR")
	if err := os.MkdirAll(filepath.Join(dir, "xdg", "aide"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// writeConfig writes modular profile files with the given context names
// (each with a no-op match rule so they are not "minimal config").
func writeConfig(t *testing.T, dir string, contexts ...string) {
	t.Helper()
	profDir := filepath.Join(dir, "xdg", "aide", "profiles")
	if err := os.MkdirAll(profDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, c := range contexts {
		body := "agent: claude\nmatch:\n  - path: /never/matches/anything\n"
		if err := os.WriteFile(filepath.Join(profDir, c+".yaml"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestContextBind_ExistingContext_AppendsMatch(t *testing.T) {
	dir := isolatedConfigDir(t)
	writeConfig(t, dir, "work")
	out, err := runContextBind(t, "work")
	if err != nil {
		t.Fatalf("unexpected error: %v\n%s", err, out)
	}
	if !strings.Contains(out, `Bound this folder to profile "work"`) {
		t.Errorf("expected success message, got: %s", out)
	}
}

func TestContextBind_MissingContext_NonTTY_Errors(t *testing.T) {
	isolatedConfigDir(t) // no contexts written
	_, err := runContextBind(t, "ghost")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, `not found`) || !strings.Contains(msg, "aide profile create ghost") {
		t.Errorf("expected not-found error pointing at create, got: %v", err)
	}
}

func TestContextBind_PathFlag_ForcesPathRule(t *testing.T) {
	dir := isolatedConfigDir(t)
	writeConfig(t, dir, "work")
	out, err := runContextBind(t, "work", "--scope", "path")
	if err != nil {
		t.Fatalf("unexpected error: %v\n%s", err, out)
	}
	if !strings.Contains(out, "by path") {
		t.Errorf("--scope path should produce path-based match: %s", out)
	}
}

func TestContextBind_RemoteFlag_OutsideGitRepo_Errors(t *testing.T) {
	dir := isolatedConfigDir(t) // tempdir is not a git repo
	writeConfig(t, dir, "work")
	_, err := runContextBind(t, "work", "--scope", "repo")
	if err == nil || !strings.Contains(err.Error(), "requires a git repository") {
		t.Errorf("expected git-repo error, got: %v", err)
	}
}

func TestContextBind_ScopeOrg_AndSubgroup(t *testing.T) {
	dir := isolatedConfigDir(t)
	writeConfig(t, dir, "work")
	mustRun(t, dir, "git", "init")
	mustRun(t, dir, "git", "remote", "add", "origin", "git@gitlab.com:acme/platform/auth/token-service.git")

	out, err := runContextBind(t, "work", "--scope", "acme/platform")
	if err != nil {
		t.Fatalf("unexpected error for subgroup scope: %v\n%s", err, out)
	}
	if !strings.Contains(out, "by remote gitlab.com/acme/platform/*") {
		t.Errorf("expected subgroup remote match, got: %s", out)
	}

	out, err = runContextBind(t, "work", "--scope", "org")
	if err != nil {
		t.Fatalf("unexpected error for org scope: %v\n%s", err, out)
	}
	if !strings.Contains(out, "by remote gitlab.com/acme/*") {
		t.Errorf("expected org remote match, got: %s", out)
	}
	// Stage D should have pruned the narrower gitlab.com/acme/platform/* rule on work.
	profBytes, err := os.ReadFile(filepath.Join(dir, "xdg", "aide", "profiles", "work.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(profBytes), "gitlab.com/acme/platform/*") {
		t.Errorf("expected narrower subgroup rule pruned after binding org:\n%s", profBytes)
	}
}

func TestContextBind_SameFolder_DifferentContext_NonTTY_Errors(t *testing.T) {
	dir := isolatedConfigDir(t)
	// "work" already covers this folder; try to bind the same path to "personal".
	var b strings.Builder
	b.WriteString("contexts:\n")
	b.WriteString("  work:\n")
	b.WriteString("    agent: claude\n")
	b.WriteString("    match:\n")
	b.WriteString("      - path: " + dir + "\n")
	b.WriteString("  personal:\n")
	b.WriteString("    agent: claude\n")
	b.WriteString("    match: []\n")
	if err := os.WriteFile(filepath.Join(dir, "xdg", "aide", "config.yaml"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := runContextBind(t, "personal", "--scope", "path")
	if err == nil || !strings.Contains(err.Error(), "already bound") {
		t.Errorf("expected already-bound error, got: %v", err)
	}
}

func TestContextBind_SameFolder_SameContext_IsNoop(t *testing.T) {
	dir := isolatedConfigDir(t)
	var b strings.Builder
	b.WriteString("contexts:\n")
	b.WriteString("  work:\n")
	b.WriteString("    agent: claude\n")
	b.WriteString("    match:\n")
	b.WriteString("      - path: " + dir + "\n")
	if err := os.WriteFile(filepath.Join(dir, "xdg", "aide", "config.yaml"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := runContextBind(t, "work", "--scope", "path")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "already bound") {
		t.Errorf("expected already-bound message, got: %s", out)
	}
}

func runContextBindInteractive(t *testing.T, input string, args ...string) (string, error) {
	t.Helper()
	origTTY := isInputTTY
	isInputTTY = func(*cobra.Command) bool { return true }
	t.Cleanup(func() { isInputTTY = origTTY })

	cmd := contextBindCmd()
	var buf bytes.Buffer
	cmd.SetIn(strings.NewReader(input))
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

func TestContextBind_InteractiveTTY_OrgScopeAndShadowingConflict(t *testing.T) {
	dir := isolatedConfigDir(t)
	profDir := filepath.Join(dir, "xdg", "aide", "profiles")
	if err := os.MkdirAll(profDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// personal has a path glob "~/*" covering dir's children; work has an older narrow repo rule.
	if err := os.WriteFile(filepath.Join(profDir, "personal.yaml"), []byte("agent: claude\nmatch:\n  - path: ~/*\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profDir, "work.yaml"), []byte("agent: claude\nmatch:\n  - remote: github.com/acme/old-repo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	repoDir := filepath.Join(dir, "billing")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(repoDir)
	mustRun(t, repoDir, "git", "init")
	mustRun(t, repoDir, "git", "remote", "add", "origin", "git@github.com:acme/billing.git")

	// Prompt 1 (scope): "2\n" -> github.com/acme/*
	// Prompt 2 (Stage B shadowing by personal path glob): "2\n" -> also bind remote: github.com/acme/billing to work
	out, err := runContextBindInteractive(t, "2\n2\n", "work")
	if err != nil {
		t.Fatalf("unexpected error: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Select match scope:") || !strings.Contains(out, "Conflict:") {
		t.Errorf("expected scope and conflict prompts in output:\n%s", out)
	}
	workBytes, err := os.ReadFile(filepath.Join(profDir, "work.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	workStr := string(workBytes)
	if !strings.Contains(workStr, "github.com/acme/*") || !strings.Contains(workStr, "github.com/acme/billing") {
		t.Errorf("expected both org glob and repo override on work:\n%s", workStr)
	}
	if strings.Contains(workStr, "github.com/acme/old-repo") {
		t.Errorf("expected old-repo pruned by Stage D:\n%s", workStr)
	}
}

func TestEmptyStateAdapter_BufferedInputPreservesTTY(t *testing.T) {
	dir := isolatedConfigDir(t)
	writeConfig(t, dir, "work")
	mustRun(t, dir, "git", "init")
	mustRun(t, dir, "git", "remote", "add", "origin", "git@github.com:acme/service.git")

	origStdinTTY := isStdinTTY
	isStdinTTY = func() bool { return true }
	t.Cleanup(func() { isStdinTTY = origStdinTTY })

	parentCmd := &cobra.Command{Use: "aide"}
	var out bytes.Buffer
	parentCmd.SetOut(&out)
	parentCmd.SetErr(&out)

	adapter := newEmptyStateAdapter(parentCmd)
	// Simulate handleEmptyState wrapping input in a *bufio.Reader and calling SetIn.
	if setter, ok := adapter.(interface{ SetIn(io.Reader) }); ok {
		setter.SetIn(strings.NewReader("2\n"))
	}
	if err := adapter.Bind("work"); err != nil {
		t.Fatalf("adapter.Bind failed: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "Select match scope:") || !strings.Contains(out.String(), "github.com/acme/*") {
		t.Errorf("expected interactive scope menu and org bind via emptyStateAdapter, got:\n%s", out.String())
	}
}

// Unused-import shield (cobra import survives even if some tests collapse).
var _ = cobra.Command{}
