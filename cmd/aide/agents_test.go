package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/jskswamy/aide/internal/output"
)

func TestAgentsList_JSONFormat(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	cfgDir := filepath.Join(xdg, "aide")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgYAML := `
agents:
  claude:
    binary: claude
contexts:
  work:
    agent: claude
`
	if err := os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte(cfgYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())

	cmd := agentsCmd()
	output.RegisterFlag(cmd)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"list", "--format", "json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v\noutput: %s", err, buf.String())
	}

	var got []agentEntry
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v\noutput: %s", err, buf.String())
	}
	found := false
	for _, e := range got {
		if e.Name == "claude" {
			found = true
			if !e.Configured {
				t.Errorf("claude should be Configured: %+v", e)
			}
			if len(e.UsedBy) != 1 || e.UsedBy[0] != "work" {
				t.Errorf("UsedBy = %v, want [work]", e.UsedBy)
			}
		}
	}
	if !found {
		t.Errorf("claude missing from JSON output: %s", buf.String())
	}
}

func TestAgentsList_JSONFormat_Empty(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Chdir(t.TempDir())
	// Empty PATH so launcher.ScanAgents finds nothing on top of the
	// empty configured-agents list, giving a deterministic zero-entry
	// result.
	t.Setenv("PATH", t.TempDir())

	cmd := agentsCmd()
	output.RegisterFlag(cmd)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"list", "--format", "json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v\noutput: %s", err, buf.String())
	}

	var got []agentEntry
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v\noutput: %s", err, buf.String())
	}
	if got == nil {
		t.Error("expected [], got JSON null")
	}
	if len(got) != 0 {
		t.Errorf("expected empty, got %+v", got)
	}
}

func TestAgentsAdd_FailedLoadLeavesConfigUntouched(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("HOME", t.TempDir())
	cfgDir := filepath.Join(xdg, "aide")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Two contexts sharing a native config dir: valid YAML that now
	// fails Load.
	orig := []byte("contexts:\n  a:\n    agent: claude\n    profile: shared\n  b:\n    agent: claude\n    profile: shared\n")
	path := filepath.Join(cfgDir, "config.yaml")
	if err := os.WriteFile(path, orig, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())

	cmd := agentsCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"add", "foo"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("agents add succeeded on a config that failed to load")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, orig) {
		t.Errorf("config.yaml was rewritten:\n%s", got)
	}
}
