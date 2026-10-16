package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

// A context that uses the default sandbox must not be reported as
// "sandbox: disabled" just because which never launched anything.
func TestWhichCmd_DefaultSandboxNotReportedDisabled(t *testing.T) {
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
	if _, ok := got["isolation_tier"]; !ok {
		t.Errorf("isolation_tier missing for a sandboxed context; the banner would say 'sandbox: disabled'\nfull output: %s", out.String())
	}
}

// `aide which --agent pi` must report the final agent's profile dir,
// the same one `aide --agent pi` would inject.
func TestWhichCmd_AgentOverrideReportsFinalAgentProfileDir(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	home := t.TempDir()
	t.Setenv("HOME", home)
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
`
	if err := os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte(cfgYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := whichCmd()
	output.RegisterFlag(cmd)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--format", "json", "--agent", "pi"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v\noutput: %s", err, out.String())
	}
	if got := out.String(); !strings.Contains(got, "PI_CODING_AGENT_DIR") ||
		!strings.Contains(got, filepath.Join(home, ".pi-work")) {
		t.Errorf("want PI_CODING_AGENT_DIR=%s in output\nfull output: %s", filepath.Join(home, ".pi-work"), got)
	}
	if strings.Contains(out.String(), "CLAUDE_CONFIG_DIR") {
		t.Errorf("CLAUDE_CONFIG_DIR reported for --agent pi\nfull output: %s", out.String())
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v\noutput: %s", err, out.String())
	}
	if got["agent_name"] != "pi" {
		t.Errorf("agent_name = %v, want pi", got["agent_name"])
	}
}
