package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"github.com/jskswamy/aide/internal/testutil"
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

// `aide status` shows the auto-included skillshare capability with the
// paths it grants read access to.
func TestStatusCmd_ShowsSkillshareReadPaths(t *testing.T) {
	xdg := testutil.CanonicalTempDir(t)
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("SKILLSHARE_CONFIG", "")
	t.Setenv("HOME", t.TempDir())
	t.Chdir(t.TempDir())
	skills := filepath.Join(xdg, "skillshare", "skills")
	if err := os.MkdirAll(skills, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(xdg, "aide"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(xdg, "aide", "config.yaml"), []byte("default_context: work\ncontexts:\n  work:\n    agent: claude\n"), 0o644); err != nil {
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
	for _, c := range got.Capabilities {
		if c.Name == "skillshare" {
			if len(c.Readable) != 1 || c.Readable[0] != skills {
				t.Errorf("skillshare readable = %v, want [%s]", c.Readable, skills)
			}
			return
		}
	}
	t.Errorf("status capabilities %v lack skillshare", got.Capabilities)
}
