// cmd/aide/context_list_test.go
package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/jskswamy/aide/internal/output"
)

func TestContextList_JSONFormat(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	cfgDir := filepath.Join(xdg, "aide")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgYAML := `
default_context: work
contexts:
  work:
    agent: claude
    secret: firmus
    match:
      - path: /repo
`
	if err := os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte(cfgYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := contextCmd()
	output.RegisterFlag(cmd)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"list", "--format", "json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v\noutput: %s", err, buf.String())
	}

	var got []contextEntry
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v\noutput: %s", err, buf.String())
	}
	if len(got) != 1 {
		t.Fatalf("got %d entries, want 1: %+v", len(got), got)
	}
	e := got[0]
	if e.Name != "work" || !e.Default || e.Agent != "claude" || e.Secret != "firmus" {
		t.Errorf("entry = %+v", e)
	}
	if len(e.Match) != 1 || e.Match[0].Path != "/repo" {
		t.Errorf("Match = %+v", e.Match)
	}
}
