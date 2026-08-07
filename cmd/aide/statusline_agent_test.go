package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLooksLikeClaudeStatuslineJSON_MatchesRealShape(t *testing.T) {
	data := []byte(`{"session_id":"abc123","model":{"id":"claude-3","display_name":"Claude"},"workspace":{"current_dir":"/tmp"}}`)
	if !looksLikeClaudeStatuslineJSON(data) {
		t.Error("expected match for Claude Code statusline JSON shape")
	}
}

func TestLooksLikeClaudeStatuslineJSON_MatchesSessionIDOnly(t *testing.T) {
	data := []byte(`{"session_id":"abc123"}`)
	if !looksLikeClaudeStatuslineJSON(data) {
		t.Error("expected match on session_id alone")
	}
}

func TestLooksLikeClaudeStatuslineJSON_RejectsUnrelatedJSON(t *testing.T) {
	data := []byte(`{"foo":"bar"}`)
	if looksLikeClaudeStatuslineJSON(data) {
		t.Error("expected no match for unrelated JSON")
	}
}

func TestLooksLikeClaudeStatuslineJSON_RejectsEmptyOrInvalid(t *testing.T) {
	if looksLikeClaudeStatuslineJSON(nil) {
		t.Error("expected no match for nil input")
	}
	if looksLikeClaudeStatuslineJSON([]byte("not json")) {
		t.Error("expected no match for invalid JSON")
	}
}

func writeContextMatchingCWD(t *testing.T, dir, contextName, agent string) {
	t.Helper()
	var b strings.Builder
	b.WriteString("contexts:\n  ")
	b.WriteString(contextName)
	b.WriteString(":\n    agent: ")
	b.WriteString(agent)
	b.WriteString("\n    match:\n      - path: ")
	b.WriteString(dir)
	b.WriteString("\n")
	path := filepath.Join(dir, "xdg", "aide", "config.yaml")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestContextAgentForCWD_ResolvesFromMatchingContext(t *testing.T) {
	dir := isolatedConfigDir(t)
	writeContextMatchingCWD(t, dir, "work", "gemini")
	got := contextAgentForCWD()
	if got != "gemini" {
		t.Errorf("got %q, want gemini", got)
	}
}

func TestContextAgentForCWD_NoConfigReturnsEmpty(t *testing.T) {
	isolatedConfigDir(t) // no config.yaml written
	got := contextAgentForCWD()
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func unsetAideAgent(t *testing.T) {
	t.Helper()
	if orig, ok := os.LookupEnv("AIDE_AGENT"); ok {
		os.Unsetenv("AIDE_AGENT")
		t.Cleanup(func() { os.Setenv("AIDE_AGENT", orig) })
	}
}

func TestResolveStatuslineAgent_ExplicitWinsOverEverything(t *testing.T) {
	t.Setenv("AIDE_AGENT", "gemini")
	got := resolveStatuslineAgent("claude", false, []byte(`{"session_id":"x"}`))
	if got != "claude" {
		t.Errorf("got %q, want claude", got)
	}
}

func TestResolveStatuslineAgent_EnvWinsOverSniffAndDefault(t *testing.T) {
	t.Setenv("AIDE_AGENT", "gemini")
	got := resolveStatuslineAgent("", false, []byte(`{"session_id":"x"}`))
	if got != "gemini" {
		t.Errorf("got %q, want gemini", got)
	}
}

func TestResolveStatuslineAgent_SniffOnlyWhenPiped(t *testing.T) {
	unsetAideAgent(t)
	got := resolveStatuslineAgent("", false, []byte(`{"session_id":"x"}`))
	if got != "claude" {
		t.Errorf("got %q, want claude (sniffed from piped JSON)", got)
	}
}

func TestResolveStatuslineAgent_SniffIgnoredOnTTY(t *testing.T) {
	unsetAideAgent(t)
	isolatedConfigDir(t) // no matching context either
	got := resolveStatuslineAgent("", true, []byte(`{"session_id":"x"}`))
	if got != "claude" {
		t.Errorf("got %q, want claude (default, not from stdin sniff on TTY)", got)
	}
}

func TestResolveStatuslineAgent_TTYUsesContextWhenAvailable(t *testing.T) {
	unsetAideAgent(t)
	dir := isolatedConfigDir(t)
	writeContextMatchingCWD(t, dir, "work", "gemini")
	got := resolveStatuslineAgent("", true, nil)
	if got != "gemini" {
		t.Errorf("got %q, want gemini", got)
	}
}

func TestResolveStatuslineAgent_DefaultsToClaudeWhenNothingResolves(t *testing.T) {
	unsetAideAgent(t)
	isolatedConfigDir(t)
	got := resolveStatuslineAgent("", true, nil)
	if got != "claude" {
		t.Errorf("got %q, want claude default", got)
	}
}
