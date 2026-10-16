package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/output"
	"github.com/jskswamy/aide/internal/trust"
)

// worktreePreviewFixture: a linked worktree whose trusted main
// checkout file grants go and a writable build dir, and whose own file
// (untrusted) asks for python.
func worktreePreviewFixture(t *testing.T) (mainDir, wtDir string) {
	t.Helper()
	mainDir, wtDir = worktreeWriteFixture(t)
	t.Setenv("HOME", t.TempDir())
	if err := os.WriteFile(filepath.Join(wtDir, ".aide.yaml"), []byte("capabilities: [python]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fc, err := trust.DefaultStore().CheckFile(filepath.Join(mainDir, ".aide.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := trust.DefaultStore().Trust(fc.Path, fc.Contents); err != nil {
		t.Fatal(err)
	}
	t.Chdir(wtDir)
	return mainDir, wtDir
}

func TestWhich_WorktreeListsInheritedGrantsAndEachTrustFile(t *testing.T) {
	mainDir, wtDir := worktreePreviewFixture(t)
	got := runSelectionJSON(t, whichCmd())

	files, _ := got["trust_files"].([]any)
	if len(files) != 1 {
		t.Fatalf("trust_files = %v, want the untrusted local file only", got["trust_files"])
	}
	f, _ := files[0].(map[string]any)
	if f["source"] != "local" || !strings.Contains(fmt.Sprint(f["path"]), filepath.Base(wtDir)) {
		t.Errorf("trust file = %v", f)
	}
	grants, _ := got["inherited_grants"].([]any)
	var seen []string
	for _, g := range grants {
		m, _ := g.(map[string]any)
		seen = append(seen, fmt.Sprintf("%v=%v", m["kind"], m["value"]))
		if !strings.Contains(fmt.Sprint(m["source"]), filepath.Base(mainDir)) {
			t.Errorf("grant %v not labeled with the main checkout file", m)
		}
	}
	want := "capability=go writable=" + filepath.Join(mainDir, "build")
	if strings.Join(seen, " ") != want {
		t.Errorf("inherited grants = %v, want %s", seen, want)
	}
}

func TestStatus_WorktreeShowsInheritedGrants(t *testing.T) {
	mainDir, _ := worktreePreviewFixture(t)
	cmd := statusCmd()
	output.RegisterFlag(cmd)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(nil)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("status: %v\n%s", err, out.String())
	}
	text := out.String()
	for _, want := range []string{"Inherited from main checkout:", filepath.Join(mainDir, "build"), "untrusted, not applied"} {
		if !strings.Contains(text, want) {
			t.Errorf("status output missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "python") {
		t.Errorf("untrusted local capability shown as active:\n%s", text)
	}
}

// A denied inherited file shows in the prompt even when the
// worktree's own file is trusted.
func TestPrompt_WorktreeUsesTheSharedGate(t *testing.T) {
	mainDir, wtDir := worktreePreviewFixture(t)
	store := trust.DefaultStore()
	fc, err := store.CheckFile(filepath.Join(wtDir, ".aide.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Trust(fc.Path, fc.Contents); err != nil {
		t.Fatal(err)
	}
	mainFile, _ := trust.CanonicalPath(filepath.Join(mainDir, ".aide.yaml"))
	if err := store.Deny(mainFile); err != nil {
		t.Fatal(err)
	}
	cmd := promptCmd()
	output.RegisterFlag(cmd)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(nil)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("prompt: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "🚫") {
		t.Errorf("prompt does not flag the denied inherited file: %q", out.String())
	}
}
