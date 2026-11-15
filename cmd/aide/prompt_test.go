package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/output"
)

func TestFormatPromptLine(t *testing.T) {
	tests := []struct {
		ctx, ctxIcon, agentIcon, gateway string
		sbDisabled                       bool
		trust                            string
		compact                          bool
		want                             string
	}{
		// icon replaces name when ctxIcon is set
		{"work", "💼", "🤖", "", false, "trusted", false, "💼 🤖 🛡"},
		{"work", "💼", "", "", false, "trusted", false, "💼 🛡"},
		// no ctxIcon: name is kept
		{"work", "", "🤖", "", false, "trusted", false, "work 🤖 🛡"},
		{"work", "", "", "", false, "trusted", false, "work 🛡"},
		{"work", "💼", "🤖", "", true, "trusted", false, "💼 🤖"},
		{"work", "💼", "🤖", "", false, "untrusted", false, "💼 🤖 ⚠"},
		{"work", "💼", "🤖", "", false, "denied", false, "💼 🤖 🚫"},
		// ESC-only icon sanitizes to "" — falls back to name
		{"work", "\x1b", "", "", false, "trusted", false, "work 🛡"},
		// ANSI sequence: ESC stripped, remaining "[2J" is safe printable text
		{"work", "", "\x1b[2J", "", false, "trusted", false, "work [2J 🛡"},
		// compact mode: no spaces between segments
		{"work", "💼", "🤖", "", false, "trusted", true, "💼🤖🛡"},
		{"work", "", "🤖", "", false, "trusted", true, "work🤖🛡"},
		{"work", "💼", "🤖", "", false, "untrusted", true, "💼🤖⚠"},
		// gateway segment: placed after agentIcon and before sandbox/trust badge
		{"work", "💼", "🤖", "⇄corp", false, "trusted", false, "💼 🤖 ⇄corp 🛡"},
		{"work", "", "🤖", "⇄login", false, "trusted", false, "work 🤖 ⇄login 🛡"},
		{"work", "💼", "🤖", "⇄?", false, "untrusted", false, "💼 🤖 ⇄? ⚠"},
		{"work", "💼", "🤖", "⇄corp", false, "trusted", true, "💼🤖⇄corp🛡"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := formatPromptLine(tt.ctx, tt.ctxIcon, tt.agentIcon, tt.gateway, tt.sbDisabled, tt.trust, tt.compact)
			if got != tt.want {
				t.Errorf("formatPromptLine = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestStarshipConfigSnippet(t *testing.T) {
	if !strings.Contains(starshipConfigSnippet, "[custom.aide]") {
		t.Error("snippet missing [custom.aide]")
	}
	if !strings.Contains(starshipConfigSnippet, "aide prompt") {
		t.Error("snippet missing aide prompt command")
	}
	if !strings.Contains(starshipConfigSnippet, "timeout") {
		t.Error("snippet missing timeout field")
	}
}

func TestPromptCmd_JSONFormat(t *testing.T) {
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
`
	if err := os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte(cfgYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())

	cmd := promptCmd()
	output.RegisterFlag(cmd)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--format", "json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v\noutput: %s", err, buf.String())
	}

	var got promptResult
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v\noutput: %s", err, buf.String())
	}
	if got.Line == "" {
		t.Errorf("expected non-empty Line: %+v", got)
	}
}

func TestPromptCmd_GatewaySegmentStatesAndPassiveZeroWrites(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	xdg := filepath.Join(home, ".config")
	t.Setenv("XDG_CONFIG_HOME", xdg)
	cfgDir := filepath.Join(xdg, "aide")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cwd := t.TempDir()
	t.Chdir(cwd)

	writeCfg := func(t *testing.T, yamlText string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte(yamlText), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runPrompt := func(t *testing.T, args ...string) string {
		t.Helper()
		cmd := promptCmd()
		var buf bytes.Buffer
		cmd.SetOut(&buf)
		cmd.SetErr(&buf)
		cmd.SetArgs(args)
		if err := cmd.Execute(); err != nil {
			t.Fatalf("prompt execute (%v): %v\noutput: %s", args, err, buf.String())
		}
		return strings.TrimSpace(buf.String())
	}

	// 1. Off by default even when gateway is chosen; --gateway flag enables it.
	writeCfg(t, `
agents:
  my-claude:
    binary: claude
    icon: "🤖"
default_context: work
contexts:
  work:
    agent: my-claude
    gateways:
      corp:
        url: https://gw.example.com/v1
        api: anthropic-messages
        store: work
        key: k
    gateway_choice:
      claude: corp
`)
	if got := runPrompt(t); got != "work 🤖 🛡" {
		t.Errorf("default (off) prompt = %q, want %q", got, "work 🤖 🛡")
	}
	if got := runPrompt(t, "--gateway"); got != "work 🤖 ⇄corp 🛡" {
		t.Errorf("--gateway prompt = %q, want %q", got, "work 🤖 ⇄corp 🛡")
	}

	// 2. preferences.prompt_gateway: true enables it; unset choice with candidates shows ⇄? and writes nothing.
	writeCfg(t, `
preferences:
  prompt_gateway: true
default_context: work
contexts:
  work:
    agent: claude
    gateways:
      corp:
        url: https://gw.example.com/v1
        api: anthropic-messages
        store: work
        key: k
`)
	if got := runPrompt(t); got != "work 🤖 ⇄? 🛡" {
		t.Errorf("unset gateway prompt = %q, want %q", got, "work 🤖 ⇄? 🛡")
	}
	// Verify zero disk writes to preferences.json or cache files.
	if _, err := os.Stat(filepath.Join(cfgDir, "preferences.json")); !os.IsNotExist(err) {
		t.Errorf("expected no preferences.json written by passive prompt, stat err = %v", err)
	}

	// 3. Explicit "none" or zero candidates shows ⇄login.
	writeCfg(t, `
preferences:
  prompt_gateway: true
default_context: work
contexts:
  work:
    agent: claude
`)
	if got := runPrompt(t); got != "work 🤖 ⇄login 🛡" {
		t.Errorf("login gateway prompt = %q, want %q", got, "work 🤖 ⇄login 🛡")
	}

	// 4. ReachNone harness (gemini) omits the gateway segment even when enabled.
	writeCfg(t, `
preferences:
  prompt_gateway: true
default_context: work
contexts:
  work:
    agent: gemini
`)
	if got := runPrompt(t); got != "work ✨ 🛡" {
		t.Errorf("ReachNone gateway prompt = %q, want %q", got, "work ✨ 🛡")
	}
}
