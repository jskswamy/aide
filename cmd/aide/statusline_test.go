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

func TestRenderStatusline_SandboxUnmanagedWhenEnvAbsent(t *testing.T) {
	cfg := config.ResolveStatusline(nil, nil)
	env := map[string]string{
		"AIDE_NETWORK_MODE": "outbound",
		"AIDE_CAPS":         "",
		"AIDE_TRUST":        "trusted",
	}
	got := renderStatusline(cfg, env)
	want := "❓ | 🌐"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRenderStatusline_NetworkUnmanagedWhenEnvAbsent(t *testing.T) {
	cfg := config.ResolveStatusline(nil, nil)
	env := map[string]string{
		"AIDE_SANDBOX": "on",
		"AIDE_CAPS":    "",
		"AIDE_TRUST":   "trusted",
	}
	got := renderStatusline(cfg, env)
	want := "🔒 | ❓"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRenderStatusline_BothUnmanagedWhenNoAideEnvAtAll(t *testing.T) {
	cfg := config.ResolveStatusline(nil, nil)
	got := renderStatusline(cfg, map[string]string{})
	want := "❓ | ❓"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestEnvForRender_AbsentVarOmittedFromMap(t *testing.T) {
	t.Setenv("AIDE_SANDBOX", "on")
	if orig, ok := os.LookupEnv("AIDE_NETWORK_MODE"); ok {
		os.Unsetenv("AIDE_NETWORK_MODE")
		t.Cleanup(func() { os.Setenv("AIDE_NETWORK_MODE", orig) })
	}
	env := envForRender()
	if _, ok := env["AIDE_SANDBOX"]; !ok {
		t.Error("AIDE_SANDBOX should be present in map when set")
	}
	if _, ok := env["AIDE_NETWORK_MODE"]; ok {
		t.Error("AIDE_NETWORK_MODE should be absent from map when unset")
	}
}

func TestRenderStatuslineModules_SingleModuleBareOutput(t *testing.T) {
	cfg := config.ResolveStatusline(nil, nil)
	env := map[string]string{
		"AIDE_SANDBOX":      "on",
		"AIDE_NETWORK_MODE": "outbound",
		"AIDE_CAPS":         "k8s,docker",
		"AIDE_TRUST":        "trusted",
	}
	got := renderStatuslineModules(cfg, env, []string{"caps"})
	want := "⚡ k8s,docker"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRenderStatuslineModules_MultipleModulesPreserveConfiguredOrder(t *testing.T) {
	cfg := config.ResolveStatusline(nil, nil)
	env := map[string]string{
		"AIDE_SANDBOX":      "on",
		"AIDE_NETWORK_MODE": "outbound",
		"AIDE_CAPS":         "",
		"AIDE_TRUST":        "trusted",
	}
	// Requested as network,sandbox but cfg.Order is sandbox,network,...
	got := renderStatuslineModules(cfg, env, []string{"network", "sandbox"})
	want := "🔒 | 🌐"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRenderStatuslineModules_EmptyModulesIsFullCombinedOutput(t *testing.T) {
	cfg := config.ResolveStatusline(nil, nil)
	env := map[string]string{
		"AIDE_SANDBOX":      "on",
		"AIDE_NETWORK_MODE": "outbound",
		"AIDE_CAPS":         "k8s",
		"AIDE_TRUST":        "trusted",
	}
	got := renderStatuslineModules(cfg, env, nil)
	want := renderStatusline(cfg, env)
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRenderStatuslineModules_AutoApproveOnlyWhenRequested(t *testing.T) {
	cfg := config.ResolveStatusline(nil, nil)
	env := map[string]string{
		"AIDE_SANDBOX":      "on",
		"AIDE_NETWORK_MODE": "outbound",
		"AIDE_AUTO_APPROVE": "1",
	}
	got := renderStatuslineModules(cfg, env, []string{"auto_approve"})
	want := "🚨"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	gotSandboxOnly := renderStatuslineModules(cfg, env, []string{"sandbox"})
	wantSandboxOnly := "🔒"
	if gotSandboxOnly != wantSandboxOnly {
		t.Errorf("got %q, want %q (auto_approve must not leak in when not requested)", gotSandboxOnly, wantSandboxOnly)
	}
}

// withPipedStdin replaces os.Stdin with a pipe pre-loaded with data,
// restoring the original on test cleanup. Simulates Claude Code piping
// statusline JSON to aide statusline's render mode.
func withPipedStdin(t *testing.T, data string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = orig })
	go func() {
		w.WriteString(data)
		w.Close()
	}()
}

func TestRunStatusline_BareCommandAutoDetectsAndRenders(t *testing.T) {
	t.Setenv("AIDE_SANDBOX", "on")
	t.Setenv("AIDE_NETWORK_MODE", "outbound")
	// Override every other AIDE_* var envForRender reads, present-but-empty,
	// so this test's exact-output assertion doesn't depend on ambient
	// process env (e.g. when go test itself runs inside an aide-launched
	// session where these are genuinely set).
	t.Setenv("AIDE_CAPS", "")
	t.Setenv("AIDE_TRUST", "")
	t.Setenv("AIDE_AUTO_APPROVE", "")
	t.Setenv("AIDE_CONTEXT", "")
	// No --agent flag is passed, so agent resolution falls through to
	// AIDE_AGENT (2nd priority in resolveStatuslineAgent) before the
	// stdin-JSON sniff this test is actually exercising gets a turn — pin
	// it to "claude" so ambient AIDE_AGENT (e.g. a real aide session) can't
	// make resolution land on an unsupported agent.
	t.Setenv("AIDE_AGENT", "claude")
	withPipedStdin(t, `{"session_id":"abc"}`)

	cmd := statuslineCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v\n%s", err, buf.String())
	}
	got := strings.TrimSpace(buf.String())
	want := "🔒 | 🌐"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRunStatusline_ModuleFlagRepeatable(t *testing.T) {
	t.Setenv("AIDE_SANDBOX", "on")
	t.Setenv("AIDE_NETWORK_MODE", "outbound")
	// Same AIDE_AGENT pin as above — no --agent flag here either.
	t.Setenv("AIDE_AGENT", "claude")
	withPipedStdin(t, `{"session_id":"abc"}`)

	cmd := statuslineCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--module", "sandbox", "--module", "network"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v\n%s", err, buf.String())
	}
	got := strings.TrimSpace(buf.String())
	want := "🔒 | 🌐"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRunStatusline_UnsupportedAgentErrors(t *testing.T) {
	withPipedStdin(t, `{"session_id":"abc"}`)
	cmd := statuslineCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--agent", "gemini"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for unsupported agent")
	}
	if !strings.Contains(err.Error(), "gemini") {
		t.Errorf("error should mention gemini, got: %v", err)
	}
}

func TestRunStatusline_ClaudeSubcommandStillWorksUnmodified(t *testing.T) {
	t.Setenv("AIDE_SANDBOX", "off")
	t.Setenv("AIDE_NETWORK_MODE", "unrestricted")
	// Same ambient-env isolation as above.
	t.Setenv("AIDE_CAPS", "")
	t.Setenv("AIDE_TRUST", "")
	t.Setenv("AIDE_AUTO_APPROVE", "")
	t.Setenv("AIDE_CONTEXT", "")
	withPipedStdin(t, `{}`)

	cmd := statuslineAgentCmd("claude")
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v\n%s", err, buf.String())
	}
	got := strings.TrimSpace(buf.String())
	want := "🔓 | 🌍"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRunStatusline_TTYRendersPreviewInsteadOfHelpText(t *testing.T) {
	t.Setenv("AIDE_SANDBOX", "on")
	t.Setenv("AIDE_NETWORK_MODE", "outbound")
	// os.Stdin defaults to the test process's real stdin, which go test
	// runs with a non-TTY (piped/redirected) stdin — simulate the TTY
	// path explicitly by pointing os.Stdin at a closed pipe end that
	// still reports as a char device is impractical in-process, so this
	// test instead verifies the behavioral contract directly:
	// resolveStatuslineAgent + renderStatuslineModules compose correctly
	// for the TTY branch (isTTY=true), which is exercised in Task 6's
	// unit tests. This test only asserts the old help-text branch is gone.
	cmd := statuslineAgentCmd("claude")
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{})
	withPipedStdin(t, `{}`)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v\n%s", err, buf.String())
	}
	if strings.Contains(buf.String(), "Render aide session state as a statusline") {
		t.Error("old help text should no longer be printed")
	}
}
