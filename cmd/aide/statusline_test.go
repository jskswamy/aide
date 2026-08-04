package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/config"
)

func boolPtrST(b bool) *bool { return &b }

// runStatuslineInstall executes `aide statusline claude --install [args...]`
// and returns stdout and any error.
func runStatuslineInstall(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := statuslineAgentCmd("claude")
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(append([]string{"--install"}, args...))
	err := cmd.Execute()
	return buf.String(), err
}

// runStatuslineRemove executes `aide statusline claude --remove [args...]`.
func runStatuslineRemove(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := statuslineAgentCmd("claude")
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(append([]string{"--remove"}, args...))
	err := cmd.Execute()
	return buf.String(), err
}

// writeStatuslineConfig writes a config.yaml with a context that has a given
// agent. Pass profile="" for a context without a profile.
func writeStatuslineConfig(t *testing.T, dir, contextName, agent, profile string) {
	t.Helper()
	var b strings.Builder
	b.WriteString("contexts:\n  ")
	b.WriteString(contextName)
	b.WriteString(":\n    agent: ")
	b.WriteString(agent)
	b.WriteString("\n    match:\n      - path: /never/matches\n")
	if profile != "" {
		b.WriteString("    profile: ")
		b.WriteString(profile)
		b.WriteString("\n")
	}
	path := filepath.Join(dir, "xdg", "aide", "config.yaml")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

// readSettingsJSON reads settings.json at path and returns the parsed map.
func readSettingsJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return m
}

func TestStatuslineInstall_DefaultContext_WritesToDefaultClaudeDir(t *testing.T) {
	dir := isolatedConfigDir(t)
	writeStatuslineConfig(t, dir, "work", "claude", "")

	out, err := runStatuslineInstall(t, "--context", "work")
	if err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Installed") {
		t.Errorf("expected Installed in output, got: %s", out)
	}

	// Default Claude dir: ~/.claude/settings.json
	settings := readSettingsJSON(t, filepath.Join(dir, ".claude", "settings.json"))
	sl, _ := settings["statusLine"].(map[string]any)
	if sl["command"] != "aide statusline claude" {
		t.Errorf("statusLine.command = %v, want aide statusline claude", sl["command"])
	}
}

func TestStatuslineInstall_ProfileContext_WritesToProfileDir(t *testing.T) {
	dir := isolatedConfigDir(t)
	writeStatuslineConfig(t, dir, "work", "claude", "work")

	out, err := runStatuslineInstall(t, "--context", "work")
	if err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Installed") {
		t.Errorf("expected Installed in output, got: %s", out)
	}

	// Profile "work" → CLAUDE_CONFIG_DIR = ~/.claude-work
	profileSettings := filepath.Join(dir, ".claude-work", "settings.json")
	settings := readSettingsJSON(t, profileSettings)
	sl, _ := settings["statusLine"].(map[string]any)
	if sl["command"] != "aide statusline claude" {
		t.Errorf("statusLine.command = %v, want aide statusline claude", sl["command"])
	}

	// Must NOT touch the default ~/.claude/settings.json
	if _, err := os.Stat(filepath.Join(dir, ".claude", "settings.json")); err == nil {
		t.Error("default ~/.claude/settings.json was written but should not have been")
	}
}

func TestStatuslineInstall_UnknownContext_ReturnsError(t *testing.T) {
	dir := isolatedConfigDir(t)
	writeStatuslineConfig(t, dir, "work", "claude", "")

	_, err := runStatuslineInstall(t, "--context", "nonexistent")
	if err == nil {
		t.Error("expected error for unknown context, got nil")
	}
}

func TestStatuslineRemove_ProfileContext_RemovesFromProfileDir(t *testing.T) {
	dir := isolatedConfigDir(t)
	writeStatuslineConfig(t, dir, "work", "claude", "work")

	// Install first.
	if _, err := runStatuslineInstall(t, "--context", "work"); err != nil {
		t.Fatalf("install: %v", err)
	}

	out, err := runStatuslineRemove(t, "--context", "work")
	if err != nil {
		t.Fatalf("remove: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Removed") {
		t.Errorf("expected Removed in output, got: %s", out)
	}

	// Key should be gone.
	settings := readSettingsJSON(t, filepath.Join(dir, ".claude-work", "settings.json"))
	if _, ok := settings["statusLine"]; ok {
		t.Error("statusLine key still present after remove")
	}
}

func TestStatuslineInstall_AlreadyInstalled_ReportsNoOp(t *testing.T) {
	dir := isolatedConfigDir(t)
	writeStatuslineConfig(t, dir, "work", "claude", "")

	if _, err := runStatuslineInstall(t, "--context", "work"); err != nil {
		t.Fatalf("first install: %v", err)
	}
	out, err := runStatuslineInstall(t, "--context", "work")
	if err != nil {
		t.Fatalf("second install: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Already installed") {
		t.Errorf("expected Already installed, got: %s", out)
	}
}

func TestRenderStatusline_AllModulesDefault(t *testing.T) {
	cfg := config.ResolveStatusline(nil, nil)
	env := map[string]string{
		"AIDE_SANDBOX":      "on",
		"AIDE_NETWORK_MODE": "outbound",
		"AIDE_CAPS":         "k8s,docker",
		"AIDE_TRUST":        "trusted",
		"AIDE_CONTEXT":      "my-project",
	}
	got := renderStatusline(cfg, env)
	want := "🔒 | 🌐 | ⚡ k8s,docker | 📁 my-project"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestRenderStatusline_UntrustedShowsTrust(t *testing.T) {
	cfg := config.ResolveStatusline(nil, nil)
	env := map[string]string{
		"AIDE_SANDBOX":      "on",
		"AIDE_NETWORK_MODE": "outbound",
		"AIDE_CAPS":         "",
		"AIDE_TRUST":        "untrusted",
	}
	got := renderStatusline(cfg, env)
	want := "🔒 | 🌐 | ⚠️"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestRenderStatusline_AutoApprovePrepended(t *testing.T) {
	cfg := config.ResolveStatusline(nil, nil)
	env := map[string]string{
		"AIDE_SANDBOX":      "on",
		"AIDE_NETWORK_MODE": "outbound",
		"AIDE_CAPS":         "",
		"AIDE_TRUST":        "trusted",
		"AIDE_AUTO_APPROVE": "1",
	}
	got := renderStatusline(cfg, env)
	want := "🚨 | 🔒 | 🌐"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestRenderStatusline_DisabledModuleSkipped(t *testing.T) {
	project := &config.StatuslineConfig{
		Network: &config.ModuleConfig{Disabled: boolPtrST(true)},
	}
	cfg := config.ResolveStatusline(nil, project)
	env := map[string]string{
		"AIDE_SANDBOX":      "on",
		"AIDE_NETWORK_MODE": "outbound",
		"AIDE_CAPS":         "",
		"AIDE_TRUST":        "trusted",
	}
	got := renderStatusline(cfg, env)
	want := "🔒"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestRenderStatusline_SandboxOff(t *testing.T) {
	cfg := config.ResolveStatusline(nil, nil)
	env := map[string]string{
		"AIDE_SANDBOX":      "off",
		"AIDE_NETWORK_MODE": "unrestricted",
		"AIDE_CAPS":         "",
		"AIDE_TRUST":        "trusted",
	}
	got := renderStatusline(cfg, env)
	want := "🔓 | 🌍"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestRenderStatusline_EmptyCapHidesModule(t *testing.T) {
	cfg := config.ResolveStatusline(nil, nil)
	env := map[string]string{
		"AIDE_SANDBOX":      "on",
		"AIDE_NETWORK_MODE": "outbound",
		"AIDE_CAPS":         "",
		"AIDE_TRUST":        "trusted",
	}
	got := renderStatusline(cfg, env)
	want := "🔒 | 🌐"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestRenderStatusline_CustomOrderRespected(t *testing.T) {
	project := &config.StatuslineConfig{
		Order: []string{"network", "sandbox"},
	}
	cfg := config.ResolveStatusline(nil, project)
	env := map[string]string{
		"AIDE_SANDBOX":      "on",
		"AIDE_NETWORK_MODE": "outbound",
		"AIDE_CAPS":         "",
		"AIDE_TRUST":        "trusted",
	}
	got := renderStatusline(cfg, env)
	want := "🌐 | 🔒"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestRenderStatusline_EmptyResultWhenAllHidden(t *testing.T) {
	project := &config.StatuslineConfig{
		Order: []string{},
	}
	cfg := config.ResolveStatusline(nil, project)
	got := renderStatusline(cfg, map[string]string{})
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}
