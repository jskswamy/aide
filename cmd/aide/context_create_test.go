// cmd/aide/context_create_test.go
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runContextCreate(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := contextCreateCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

func TestContextCreate_FullyScripted_NoHere(t *testing.T) {
	dir := isolatedConfigDir(t)
	out, err := runContextCreate(t, "work", "--agent", "claude", "--no-here")
	if err != nil {
		t.Fatalf("unexpected error: %v\n%s", err, out)
	}
	if !strings.Contains(out, `Created context "work"`) {
		t.Errorf("expected create message, got: %s", out)
	}
	// Verify the config file was written and the context has no match rules.
	cfgBytes, err := os.ReadFile(filepath.Join(dir, "xdg", "aide", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cfgBytes), "work:") {
		t.Errorf("config did not include new context: %s", cfgBytes)
	}
	if strings.Contains(string(cfgBytes), "match:") {
		t.Errorf("--no-here should produce no match rules: %s", cfgBytes)
	}
}

func TestContextCreate_FullyScripted_WithHere(t *testing.T) {
	dir := isolatedConfigDir(t)
	out, err := runContextCreate(t, "work", "--agent", "claude", "--here")
	if err != nil {
		t.Fatalf("unexpected error: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Bound this folder") {
		t.Errorf("--here should bind cwd: %s", out)
	}
	cfgBytes, _ := os.ReadFile(filepath.Join(dir, "xdg", "aide", "config.yaml"))
	if !strings.Contains(string(cfgBytes), "match:") {
		t.Errorf("--here should produce a match rule: %s", cfgBytes)
	}
}

func TestContextCreate_NonTTY_NoName_Errors(t *testing.T) {
	isolatedConfigDir(t)
	_, err := runContextCreate(t, "--agent", "claude", "--no-here")
	if err == nil || !strings.Contains(err.Error(), "name is required") {
		t.Errorf("expected name-required error in non-TTY, got: %v", err)
	}
}

func TestContextCreate_NonTTY_NoAgent_Errors(t *testing.T) {
	isolatedConfigDir(t)
	// We do not stub agent autodetect; in CI no claude/codex/etc is on PATH,
	// so no agent will be auto-picked. The test expects a clear error.
	_, err := runContextCreate(t, "work", "--no-here")
	if err == nil {
		t.Fatal("expected an error when no agent can be resolved")
	}
	if !strings.Contains(err.Error(), "agent") {
		t.Errorf("error must mention agent: %v", err)
	}
}

func TestContextCreate_DuplicateName_Errors(t *testing.T) {
	dir := isolatedConfigDir(t)
	writeConfig(t, dir, "work")
	_, err := runContextCreate(t, "work", "--agent", "claude", "--no-here")
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected duplicate error, got: %v", err)
	}
}
