package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/jskswamy/aide/internal/output"
)

func TestWhichCmd_JSONFormat(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	proj := t.TempDir()
	t.Chdir(proj)

	cfgDir := filepath.Join(xdg, "aide")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgYAML := `
default_context: work
contexts:
  work:
    agent: claude
`
	if err := os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte(cfgYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := whichCmd()
	output.RegisterFlag(cmd)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--format", "json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v\noutput: %s", err, out.String())
	}

	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v\noutput: %s", err, out.String())
	}
	if got["context_name"] != "work" {
		t.Errorf("context_name = %v, want %q\nfull output: %s", got["context_name"], "work", out.String())
	}
	if got["agent_name"] != "claude" {
		t.Errorf("agent_name = %v, want %q\nfull output: %s", got["agent_name"], "claude", out.String())
	}
}
