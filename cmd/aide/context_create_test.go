// cmd/aide/context_create_test.go
package main

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/config"
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
	if !strings.Contains(out, `Created profile "work"`) {
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

func TestProfileSetDefault_ProfileFlag(t *testing.T) {
	dir := isolatedConfigDir(t)
	writeConfig(t, dir, "work", "personal")
	run := func(args ...string) (string, error) {
		cmd := contextSetDefaultCmd()
		var buf bytes.Buffer
		cmd.SetOut(&buf)
		cmd.SetErr(&buf)
		cmd.SetArgs(args)
		err := cmd.Execute()
		return buf.String(), err
	}
	if out, err := run("--profile", "personal"); err != nil || !strings.Contains(out, `Set default profile to "personal"`) {
		t.Fatalf("set-default --profile personal: %v\n%s", err, out)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "xdg", "aide", "config.yaml"))
	if !strings.Contains(string(data), "default_context: personal") {
		t.Errorf("default not saved (legacy file keeps its schema):\n%s", data)
	}
	if _, err := run("work", "--profile", "personal"); err == nil {
		t.Error("argument and --profile naming different profiles should conflict")
	}
}

// The first profile on a harness keeps its global login; a second one
// gets its own config dirs, so neither launch is refused for sharing.
func TestContextCreate_SecondProfileGetsOwnConfigDir(t *testing.T) {
	dir := isolatedConfigDir(t)
	out, err := runContextCreate(t, "work", "--agent", "claude", "--no-here")
	if err != nil || strings.Contains(out, "own config dirs") {
		t.Fatalf("first profile: err=%v\n%s", err, out)
	}
	out, err = runContextCreate(t, "personal", "--agent", "claude", "--no-here")
	if err != nil {
		t.Fatalf("second profile: %v\n%s", err, out)
	}
	if !strings.Contains(out, `Profile "personal" gets its own config dirs (`) {
		t.Errorf("no isolation note:\n%s", out)
	}
	cfg, err := config.Load(filepath.Join(dir, "xdg", "aide"), dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Contexts["work"].SelectsConfigDir() || cfg.Contexts["personal"].Profile != "personal" {
		t.Errorf("profiles = %+v", cfg.Contexts)
	}
	if err := cfg.GlobalLoginError("work", "claude", dir); err != nil {
		t.Errorf("launch would still be refused: %v", err)
	}
}

// aide use and the setup wizard create profiles the same way.
func TestUseAndSetup_SecondProfileGetsOwnConfigDir(t *testing.T) {
	dir := isolatedConfigDir(t)
	runIn := func(sub string, run func() (string, error)) string {
		t.Helper()
		wd := filepath.Join(dir, sub)
		if err := os.MkdirAll(wd, 0o755); err != nil {
			t.Fatal(err)
		}
		t.Chdir(wd)
		out, err := run()
		if err != nil {
			t.Fatalf("%s: %v\n%s", sub, err, out)
		}
		return out
	}
	use := func() (string, error) {
		cmd := useCmd()
		var buf bytes.Buffer
		cmd.SetOut(&buf)
		cmd.SetErr(&buf)
		cmd.SetArgs([]string{"claude"})
		err := cmd.Execute()
		return buf.String(), err
	}
	if out := runIn("first", use); strings.Contains(out, "own config dirs") {
		t.Errorf("first profile isolated:\n%s", out)
	}
	if out := runIn("second", use); !strings.Contains(out, `Profile "second" gets its own config dirs`) {
		t.Errorf("aide use: no isolation note:\n%s", out)
	}
	out := runIn("third", func() (string, error) {
		cfg, err := config.Load(filepath.Join(dir, "xdg", "aide"), dir)
		if err != nil {
			return "", err
		}
		wd, _ := os.Getwd()
		var buf bytes.Buffer
		err = setupCreateNew(&buf, bufio.NewReader(strings.NewReader(strings.Repeat("claude\n", 1)+strings.Repeat("\n", 20))), cfg, wd)
		return buf.String(), err
	})
	if !strings.Contains(out, `Profile "third" gets its own config dirs`) {
		t.Errorf("setup: no isolation note:\n%s", out)
	}
	cfg, err := config.Load(filepath.Join(dir, "xdg", "aide"), dir)
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"first": "", "second": "second", "third": "third"} {
		if got := cfg.Contexts[name].Profile; got != want {
			t.Errorf("%s: config_dir_name = %q, want %q", name, got, want)
		}
	}
}
