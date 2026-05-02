// cmd/aide/context_bind_test.go
package main

import (
	"bytes"
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
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg"))
	t.Setenv("HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, "xdg", "aide"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// writeConfig writes a minimal config.yaml with the given context names
// (each with a no-op match rule so they are not "minimal config").
func writeConfig(t *testing.T, dir string, contexts ...string) {
	t.Helper()
	var b strings.Builder
	b.WriteString("contexts:\n")
	for _, c := range contexts {
		b.WriteString("  ")
		b.WriteString(c)
		b.WriteString(":\n")
		b.WriteString("    agent: claude\n")
		b.WriteString("    match:\n")
		b.WriteString("      - path: /never/matches/anything\n")
	}
	path := filepath.Join(dir, "xdg", "aide", "config.yaml")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestContextBind_ExistingContext_AppendsMatch(t *testing.T) {
	dir := isolatedConfigDir(t)
	writeConfig(t, dir, "work")
	out, err := runContextBind(t, "work")
	if err != nil {
		t.Fatalf("unexpected error: %v\n%s", err, out)
	}
	if !strings.Contains(out, `Bound this folder to context "work"`) {
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
	if !strings.Contains(msg, `not found`) || !strings.Contains(msg, "aide context create ghost") {
		t.Errorf("expected not-found error pointing at create, got: %v", err)
	}
}

func TestContextBind_PathFlag_ForcesPathRule(t *testing.T) {
	dir := isolatedConfigDir(t)
	writeConfig(t, dir, "work")
	out, err := runContextBind(t, "work", "--path")
	if err != nil {
		t.Fatalf("unexpected error: %v\n%s", err, out)
	}
	if !strings.Contains(out, "by path") {
		t.Errorf("--path should produce path-based match: %s", out)
	}
}

func TestContextBind_RemoteFlag_OutsideGitRepo_Errors(t *testing.T) {
	isolatedConfigDir(t) // tempdir is not a git repo
	writeConfig(t, isolatedConfigDirReuse(t), "work") // sub-helper not needed; test isolation is per-test
	// The test above already isolated; we only care about the error.
	_, err := runContextBind(t, "work", "--remote")
	if err == nil || !strings.Contains(err.Error(), "not a git repo") {
		t.Errorf("expected git-repo error, got: %v", err)
	}
}

// Helper used by the remote-flag test to avoid re-isolating.
func isolatedConfigDirReuse(t *testing.T) string {
	t.Helper()
	cwd, _ := os.Getwd()
	return cwd
}

func TestContextBind_PathAndRemote_MutualExclusive(t *testing.T) {
	dir := isolatedConfigDir(t)
	writeConfig(t, dir, "work")
	_, err := runContextBind(t, "work", "--path", "--remote")
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("expected mutual-exclusion error, got: %v", err)
	}
}

// Unused-import shield (cobra import survives even if some tests collapse).
var _ = cobra.Command{}
