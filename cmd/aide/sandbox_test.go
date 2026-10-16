package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/launcher"
	"github.com/jskswamy/aide/internal/output"
)

func runSandboxCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var buf bytes.Buffer
	cmd := sandboxCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

func runSandboxCmdFormat(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var buf bytes.Buffer
	cmd := sandboxCmd()
	output.RegisterFlag(cmd)
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

func TestSandboxList_JSONFormat(t *testing.T) {
	out, err := runSandboxCmdFormat(t, "list", "--format", "json")
	if err != nil {
		t.Fatalf("execute: %v\noutput: %s", err, out)
	}
	var got []sandboxProfileEntry
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal: %v\noutput: %s", err, out)
	}
	found := false
	for _, e := range got {
		if e.Name == "default" {
			found = true
		}
	}
	if !found {
		t.Errorf("default profile missing: %s", out)
	}
}

func TestSandboxTypes_JSONFormat(t *testing.T) {
	out, err := runSandboxCmdFormat(t, "types", "--format", "json")
	if err != nil {
		t.Fatalf("execute: %v\noutput: %s", err, out)
	}
	var got []sandboxTypeEntry
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal: %v\noutput: %s", err, out)
	}
	if len(got) != 3 {
		t.Fatalf("got %d entries, want 3: %+v", len(got), got)
	}
	if got[0].Type != "always" || !got[0].DefaultOn {
		t.Errorf("entries[0] = %+v", got[0])
	}
}

// writeClaudeAgentConfig writes a minimal global config.yaml declaring
// one context ("work", the default) using the given agent name.
func writeClaudeAgentConfig(t *testing.T, dir, agent string) {
	t.Helper()
	yaml := "default_context: work\ncontexts:\n  work:\n    agent: " + agent + "\n"
	if err := os.WriteFile(filepath.Join(dir, "xdg", "aide", "config.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readAdditionalDirs(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	perms, _ := raw["permissions"].(map[string]interface{})
	rawDirs, _ := perms["additionalDirectories"].([]interface{})
	out := make([]string, 0, len(rawDirs))
	for _, d := range rawDirs {
		if s, ok := d.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func TestSandboxAllow_GrantsClaudeProjectDirectory(t *testing.T) {
	dir := isolatedConfigDir(t)
	writeClaudeAgentConfig(t, dir, "claude")

	out, err := runSandboxCmd(t, "allow", "/repo/extra")
	if err != nil {
		t.Fatalf("sandbox allow: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Added /repo/extra to claude's additionalDirectories") {
		t.Errorf("missing agent-grant confirmation; got:\n%s", out)
	}

	got := readAdditionalDirs(t, filepath.Join(dir, ".claude", "settings.local.json"))
	if len(got) != 1 || got[0] != "/repo/extra" {
		t.Fatalf("additionalDirectories = %v, want [/repo/extra]", got)
	}
}

func TestSandboxAllow_GlobalScope_WritesUserSettingsNotProject(t *testing.T) {
	dir := isolatedConfigDir(t)
	writeClaudeAgentConfig(t, dir, "claude")

	out, err := runSandboxCmd(t, "allow", "/repo/extra", "--global")
	if err != nil {
		t.Fatalf("sandbox allow: %v\n%s", err, out)
	}

	got := readAdditionalDirs(t, filepath.Join(sandboxedClaudeConfigDir(dir), "settings.json"))
	if len(got) != 1 || got[0] != "/repo/extra" {
		t.Fatalf("additionalDirectories = %v, want [/repo/extra]", got)
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".claude", "settings.local.json")); statErr == nil {
		t.Errorf("global grant should not touch project settings.local.json")
	}
}

// sandboxedClaudeConfigDir returns the user-scope config dir a sandboxed
// Claude reads under home: the launch-time CLAUDE_CONFIG_DIR redirect
// where the platform applies one (Linux), else Claude's default ~/.claude.
func sandboxedClaudeConfigDir(home string) string {
	if dir := launcher.SandboxAgentEnv("claude", home, os.Environ())["CLAUDE_CONFIG_DIR"]; dir != "" {
		return dir
	}
	return filepath.Join(home, ".claude")
}

func TestSandboxAllow_NoAgentGrantFlagSkipsClaudeWrite(t *testing.T) {
	dir := isolatedConfigDir(t)
	writeClaudeAgentConfig(t, dir, "claude")

	out, err := runSandboxCmd(t, "allow", "/repo/extra", "--no-agent-grant")
	if err != nil {
		t.Fatalf("sandbox allow: %v\n%s", err, out)
	}
	if strings.Contains(out, "additionalDirectories") {
		t.Errorf("expected no agent-grant output with --no-agent-grant; got:\n%s", out)
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".claude", "settings.local.json")); statErr == nil {
		t.Errorf("--no-agent-grant should not create Claude's settings.local.json")
	}
}

func TestSandboxAllow_UnsupportedAgentSkipsSilently(t *testing.T) {
	dir := isolatedConfigDir(t)
	writeClaudeAgentConfig(t, dir, "codex")

	out, err := runSandboxCmd(t, "allow", "/repo/extra")
	if err != nil {
		t.Fatalf("sandbox allow: %v\n%s", err, out)
	}
	if strings.Contains(out, "additionalDirectories") {
		t.Errorf("codex has no DirectoryGranter; expected no agent-grant output, got:\n%s", out)
	}
}

// writeClaudeAgentConfigWithContext writes a global config.yaml declaring
// two contexts: the default "work" (plain claude), and a second named
// context using the given agent and a custom CLAUDE_CONFIG_DIR env value
// (tilde-prefixed, to exercise homepath expansion via resolveContextEnv).
func writeClaudeAgentConfigWithContext(t *testing.T, dir, contextName, agent, configDirEnv string) {
	t.Helper()
	yaml := "default_context: work\ncontexts:\n" +
		"  work:\n    agent: " + agent + "\n" +
		"  " + contextName + ":\n    agent: " + agent + "\n" +
		"    env:\n      CLAUDE_CONFIG_DIR: " + configDirEnv + "\n"
	if err := os.WriteFile(filepath.Join(dir, "xdg", "aide", "config.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSandboxAllow_ContextFlag_GrantsIntoNamedContextProfile(t *testing.T) {
	dir := isolatedConfigDir(t)
	writeClaudeAgentConfigWithContext(t, dir, "personal", "claude", "~/profile-personal")

	out, err := runSandboxCmd(t, "allow", "/repo/extra", "--global", "--profile", "personal")
	if err != nil {
		t.Fatalf("sandbox allow: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Added /repo/extra to claude's additionalDirectories") {
		t.Errorf("missing agent-grant confirmation; got:\n%s", out)
	}

	// Grant must land in "personal"'s CLAUDE_CONFIG_DIR, not the default
	// ~/.claude used by the "work" context that cwd would otherwise
	// resolve to.
	got := readAdditionalDirs(t, filepath.Join(dir, "profile-personal", "settings.json"))
	if len(got) != 1 || got[0] != "/repo/extra" {
		t.Fatalf("additionalDirectories = %v, want [/repo/extra] under profile-personal", got)
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".claude", "settings.json")); statErr == nil {
		t.Errorf("--context personal should not write to the default ~/.claude/settings.json")
	}
}

func TestSandboxDeny_RevokesClaudeProjectDirectory(t *testing.T) {
	dir := isolatedConfigDir(t)
	writeClaudeAgentConfig(t, dir, "claude")

	if out, err := runSandboxCmd(t, "allow", "/repo/extra/sub"); err != nil {
		t.Fatalf("sandbox allow: %v\n%s", err, out)
	}

	out, err := runSandboxCmd(t, "deny", "/repo/extra")
	if err != nil {
		t.Fatalf("sandbox deny: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Removed /repo/extra from claude's additionalDirectories") {
		t.Errorf("missing agent-revoke confirmation; got:\n%s", out)
	}

	got := readAdditionalDirs(t, filepath.Join(dir, ".claude", "settings.local.json"))
	if len(got) != 0 {
		t.Fatalf("additionalDirectories = %v, want empty (nested grant should be revoked)", got)
	}
}

// aide sandbox test must print what a profile launch applies: the
// profile's own Claude config dir, not the profile-less default.
func TestSandboxTest_ProfileUsesItsOwnClaudeConfigDir(t *testing.T) {
	dir := isolatedConfigDir(t)
	yaml := "profiles:\n" +
		"  verify-a:\n    agent: claude\n    config_dir_name: verify-a\n" +
		"  verify-b:\n    agent: claude\n    config_dir_name: verify-b\n" +
		"default_profile: verify-a\n"
	if err := os.WriteFile(filepath.Join(dir, "xdg", "aide", "config.yaml"), []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := runSandboxCmd(t, "test", "--profile", "verify-b")
	if err != nil {
		t.Fatalf("sandbox test: %v\n%s", err, out)
	}
	if !strings.Contains(out, filepath.Join(dir, ".claude-verify-b")) {
		t.Errorf("profile's config dir missing from sandbox profile:\n%s", out)
	}
	for _, other := range []string{".claude-verify-a", filepath.Join(".config", "aide", "claude")} {
		if strings.Contains(out, other) {
			t.Errorf("sandbox profile for verify-b grants %s:\n%s", other, out)
		}
	}
}
