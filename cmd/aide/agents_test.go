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
