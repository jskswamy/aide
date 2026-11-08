package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/jskswamy/aide/internal/output"
)

func writeListConfig(t *testing.T) {
	t.Helper()
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
    secret: personal
    match:
      - path: /repo
`
	if err := os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte(cfgYAML), 0o644); err != nil {
		t.Fatal(err)
	}

}

// runList runs `<group> list --format json` and returns stdout and
// stderr separately.
func runList(t *testing.T, group *cobra.Command) (string, string) {
	t.Helper()
	output.RegisterFlag(group)
	var out, errOut bytes.Buffer
	group.SetOut(&out)
	group.SetErr(&errOut)
	group.SetArgs([]string{"list", "--format", "json"})
	if err := group.Execute(); err != nil {
		t.Fatalf("execute: %v\noutput: %s%s", err, out.String(), errOut.String())
	}
	return out.String(), errOut.String()
}

func TestProfileList_JSONFormat(t *testing.T) {
	writeListConfig(t)
	stdout, stderr := runList(t, profileCmd())
	if stderr != "" {
		t.Errorf("aide profile printed to stderr: %q", stderr)
	}
	checkListJSON(t, stdout)
}

// aide context is the deprecated alias: same output, plus a notice on
// stderr only, so JSON on stdout stays parseable.
func TestContextAlias_ListPrintsDeprecationOnStderr(t *testing.T) {
	writeListConfig(t)
	stdout, stderr := runList(t, contextCmd())
	if !strings.Contains(stderr, "aide context is deprecated; use aide profile") {
		t.Errorf("stderr = %q, want the deprecation notice", stderr)
	}
	checkListJSON(t, stdout)
}

func checkListJSON(t *testing.T, stdout string) {
	t.Helper()
	buf := bytes.NewBufferString(stdout)
	var got []contextEntry
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v\noutput: %s", err, buf.String())
	}
	if len(got) != 1 {
		t.Fatalf("got %d entries, want 1: %+v", len(got), got)
	}
	e := got[0]
	if e.Name != "work" || !e.Default || e.Agent != "claude" || e.Secret != "personal" {
		t.Errorf("entry = %+v", e)
	}
	if len(e.Match) != 1 || e.Match[0].Path != "/repo" {
		t.Errorf("Match = %+v", e.Match)
	}
}

func TestProfileList_SynthesizedDefaultMarkedDefault(t *testing.T) {
	dir := isolatedConfigDir(t)
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "claude"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)

	stdout, stderr := runList(t, profileCmd())
	if stderr != "" {
		t.Errorf("aide profile printed to stderr: %q", stderr)
	}
	var got []contextEntry
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("unmarshal: %v\noutput: %s", err, stdout)
	}
	if len(got) != 1 || got[0].Name != "default" || !got[0].Default || got[0].Agent != "claude" {
		t.Errorf("got %+v, want single default profile marked Default=true", got)
	}
}
