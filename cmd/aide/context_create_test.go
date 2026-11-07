// cmd/aide/context_create_test.go
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

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
	// Verify the profile file was written and the context has no match rules.
	cfgBytes, err := os.ReadFile(filepath.Join(dir, "xdg", "aide", "profiles", "work.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cfgBytes), "agent: claude") {
		t.Errorf("profile file did not include new context: %s", cfgBytes)
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
	cfgBytes, _ := os.ReadFile(filepath.Join(dir, "xdg", "aide", "profiles", "work.yaml"))
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
	if !strings.Contains(string(data), "default_profile: personal") {
		t.Errorf("default not saved:\n%s", data)
	}
	if _, err := run("work", "--profile", "personal"); err == nil {
		t.Error("argument and --profile naming different profiles should conflict")
	}
}

// Every created profile automatically defaults its config_dir_name to the
// profile name, while omitting redundant config_dir_name on disk.
func TestContextCreate_ProfilesDefaultToOwnConfigDir(t *testing.T) {
	dir := isolatedConfigDir(t)
	if out, err := runContextCreate(t, "work", "--agent", "claude", "--no-here"); err != nil {
		t.Fatalf("first profile: err=%v\n%s", err, out)
	}
	if out, err := runContextCreate(t, "personal", "--agent", "claude", "--no-here"); err != nil {
		t.Fatalf("second profile: %v\n%s", err, out)
	}
	cfg, err := config.Load(filepath.Join(dir, "xdg", "aide"), dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Contexts["work"].Profile != "work" || cfg.Contexts["personal"].Profile != "personal" {
		t.Errorf("profiles = %+v", cfg.Contexts)
	}
	workRaw, err := os.ReadFile(filepath.Join(dir, "xdg", "aide", "profiles", "work.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(workRaw), "config_dir_name") {
		t.Errorf("work.yaml should omit default config_dir_name:\n%s", workRaw)
	}
}

// aide use and the deprecated setup forwarder create profiles with default per-profile isolation.
func TestUseAndSetup_ProfilesDefaultToOwnConfigDir(t *testing.T) {
	dir := isolatedConfigDir(t)
	if !useCmd().Hidden {
		t.Error("expected useCmd to be Hidden: true")
	}
	if !setupCmd().Hidden {
		t.Error("expected setupCmd to be Hidden: true")
	}

	runIn := func(sub string, run func() (string, string, error)) (string, string) {
		t.Helper()
		wd := filepath.Join(dir, sub)
		if err := os.MkdirAll(wd, 0o755); err != nil {
			t.Fatal(err)
		}
		t.Chdir(wd)
		stdout, stderr, err := run()
		if err != nil {
			t.Fatalf("%s: %v\nstdout: %s\nstderr: %s", sub, err, stdout, stderr)
		}
		return stdout, stderr
	}
	use := func() (string, string, error) {
		cmd := useCmd()
		var outBuf, errBuf bytes.Buffer
		cmd.SetOut(&outBuf)
		cmd.SetErr(&errBuf)
		cmd.SetArgs([]string{"claude"})
		err := cmd.Execute()
		return outBuf.String(), errBuf.String(), err
	}
	_, stderr1 := runIn("first", use)
	if !strings.Contains(stderr1, "aide use is deprecated; use 'aide profile bind'") {
		t.Errorf("expected use deprecation warning on stderr, got: %q", stderr1)
	}
	_, stderr2 := runIn("second", use)
	if !strings.Contains(stderr2, "aide use is deprecated; use 'aide profile bind'") {
		t.Errorf("expected use deprecation warning on stderr, got: %q", stderr2)
	}

	origTTY := isInputTTY
	isInputTTY = func(_ *cobra.Command) bool { return true }
	t.Cleanup(func() { isInputTTY = origTTY })

	// Place only "claude" on PATH so singleAgentOnPath auto-detects it.
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "claude"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)

	// With 2 existing profiles (first, second), setupCmd delegates to contextBindCmd,
	// which offers [3] Create a new profile -> name "third", secret ""
	stdout3, stderr3 := runIn("third", func() (string, string, error) {
		cmd := setupCmd()
		var outBuf, errBuf bytes.Buffer
		cmd.SetIn(strings.NewReader("3\nthird\n\n"))
		cmd.SetOut(&outBuf)
		cmd.SetErr(&errBuf)
		err := cmd.Execute()
		return outBuf.String(), errBuf.String(), err
	})
	if !strings.Contains(stderr3, "aide setup is deprecated; use 'aide profile bind'") {
		t.Errorf("expected setup deprecation warning on stderr, got: %q", stderr3)
	}
	if !strings.Contains(stdout3, "[3] Create a new profile") {
		t.Errorf("expected [3] Create a new profile in bind picker, got:\n%s", stdout3)
	}

	cfg, err := config.Load(filepath.Join(dir, "xdg", "aide"), dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"first", "second", "third"} {
		if got := cfg.Contexts[name].Profile; got != name {
			t.Errorf("%s: config_dir_name = %q, want %q", name, got, name)
		}
	}
}

func TestSetupCmd_NoProfiles_DelegatesToCreate(t *testing.T) {
	dir := isolatedConfigDir(t)

	origTTY := isInputTTY
	isInputTTY = func(_ *cobra.Command) bool { return true }
	t.Cleanup(func() { isInputTTY = origTTY })

	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "claude"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)

	// No profiles exist -> setupCmd delegates to contextCreateCmd.
	// Prompts: profile name ("work"), secret store (""), bind here ("y")
	cmd := setupCmd()
	var outBuf, errBuf bytes.Buffer
	cmd.SetIn(strings.NewReader("work\n\ny\n"))
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("setupCmd failed: %v\nstdout: %s\nstderr: %s", err, outBuf.String(), errBuf.String())
	}
	if !strings.Contains(errBuf.String(), "aide setup is deprecated; use 'aide profile bind'") {
		t.Errorf("expected setup deprecation warning on stderr, got: %q", errBuf.String())
	}
	if !strings.Contains(outBuf.String(), `Created profile "work"`) {
		t.Errorf("expected Created profile \"work\", got:\n%s", outBuf.String())
	}
}

func TestContextBind_NoArgs_CreateNewProfileOption(t *testing.T) {
	dir := isolatedConfigDir(t)
	writeConfig(t, dir, "default")

	origTTY := isInputTTY
	isInputTTY = func(_ *cobra.Command) bool { return true }
	t.Cleanup(func() { isInputTTY = origTTY })

	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "claude"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)

	// Menu has [1] default, [2] Create a new profile.
	// Select 2 -> profile name "work", secret store "". Because here=tristateYes, it binds immediately.
	cmd := contextBindCmd()
	var outBuf bytes.Buffer
	cmd.SetIn(strings.NewReader("2\nwork\n\n"))
	cmd.SetOut(&outBuf)
	cmd.SetErr(&outBuf)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("contextBindCmd failed: %v\n%s", err, outBuf.String())
	}
	out := outBuf.String()
	if !strings.Contains(out, "[2] Create a new profile") {
		t.Errorf("expected [2] Create a new profile in menu, got:\n%s", out)
	}
	if !strings.Contains(out, `Created profile "work"`) || !strings.Contains(out, "Bound this folder") {
		t.Errorf("expected profile created and folder bound, got:\n%s", out)
	}
}

func TestInitCmd_SecretsSetup_DefaultNameAndResolveRecipient(t *testing.T) {
	dir := isolatedConfigDir(t)
	cfgDir := filepath.Join(dir, "xdg", "aide")
	unsetEnv(t, "SOPS_AGE_KEY")
	unsetEnv(t, "SOPS_AGE_KEY_FILE")

	origTTY := isInputTTY
	isInputTTY = func(_ *cobra.Command) bool { return true }
	t.Cleanup(func() { isInputTTY = origTTY })

	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "claude"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	editorScript := filepath.Join(binDir, "fake-editor")
	if err := os.WriteFile(editorScript, []byte("#!/bin/sh\nprintf 'api_key: test-secret\\n' > \"$1\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	t.Setenv("EDITOR", editorScript)

	// Prompts:
	// 1. Primary agent (default: claude): ""
	// 2. Set up secrets? (y/N): "y"
	// 3. Secrets file name (default: default): ""
	// 4. ResolveRecipient (0 keys on TTY -> Select [1] to generate): "1"
	cmd := initCmd()
	var out bytes.Buffer
	cmd.SetIn(strings.NewReader("\ny\n\n1\n"))
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("initCmd with secrets failed: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "Secrets file name (default: default): ") {
		t.Errorf("expected default secrets file name prompt, got:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "Created secrets/default.enc.yaml") {
		t.Errorf("expected Created secrets/default.enc.yaml, got:\n%s", out.String())
	}
	cfg, err := config.Load(cfgDir, dir)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if got := cfg.Contexts["default"].Secret; got != "default" {
		t.Errorf("default profile secret = %q, want %q", got, "default")
	}
}

func TestContextCreate_InteractiveTTY_OrgScope(t *testing.T) {
	dir := isolatedConfigDir(t)
	mustRun(t, dir, "git", "init")
	mustRun(t, dir, "git", "remote", "add", "origin", "git@github.com:acme/new-service.git")

	origTTY := isInputTTY
	isInputTTY = func(_ *cobra.Command) bool { return true }
	t.Cleanup(func() { isInputTTY = origTTY })

	// Prompts: secret store (""), bind here ("y"), scope ("2" -> github.com/acme/*)
	cmd := contextCreateCmd()
	var buf bytes.Buffer
	cmd.SetIn(strings.NewReader("\ny\n2\n"))
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"work", "--agent", "claude"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v\n%s", err, buf.String())
	}
	if !strings.Contains(buf.String(), "by remote github.com/acme/*") {
		t.Errorf("expected org remote bound, got:\n%s", buf.String())
	}
}
