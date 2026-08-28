package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

	got := readAdditionalDirs(t, filepath.Join(dir, ".claude", "settings.json"))
	if len(got) != 1 || got[0] != "/repo/extra" {
		t.Fatalf("additionalDirectories = %v, want [/repo/extra]", got)
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".claude", "settings.local.json")); statErr == nil {
		t.Errorf("global grant should not touch project settings.local.json")
	}
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

	out, err := runSandboxCmd(t, "allow", "/repo/extra", "--global", "--context", "personal")
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
