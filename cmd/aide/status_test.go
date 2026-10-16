package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/jskswamy/aide/internal/output"
	"github.com/jskswamy/aide/internal/provision"
)

func TestStatusCmd_JSONFormat(t *testing.T) {
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

	cmd := statusCmd()
	output.RegisterFlag(cmd)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--format", "json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v\noutput: %s", err, out.String())
	}

	var got statusResult
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v\noutput: %s", err, out.String())
	}
	if got.Context != "work" {
		t.Errorf("Context = %q, want %q", got.Context, "work")
	}
	if got.Agent != "claude" {
		t.Errorf("Agent = %q, want %q", got.Agent, "claude")
	}
}

// `aide status --agent pi` must validate the profile against pi, the
// agent that would run, not the context's claude.
func TestStatusCmd_AgentOverrideValidatesProfileAgainstFinalAgent(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("HOME", t.TempDir())
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
    profile: work
    env:
      PI_CODING_AGENT_DIR: /elsewhere
`
	if err := os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte(cfgYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := statusCmd()
	output.RegisterFlag(cmd)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--format", "json", "--agent", "pi"})

	err := cmd.Execute()
	if !errors.Is(err, provision.ErrProfileConflict) {
		t.Fatalf("execute error = %v, want ErrProfileConflict\noutput: %s", err, out.String())
	}
}
