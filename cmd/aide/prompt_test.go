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
		ctx, ctxIcon, agentIcon string
		sbDisabled              bool
		trust                   string
		compact                 bool
		want                    string
	}{
		// icon replaces name when ctxIcon is set
		{"work", "💼", "🤖", false, "trusted", false, "💼 🤖 🛡"},
		{"work", "💼", "", false, "trusted", false, "💼 🛡"},
		// no ctxIcon: name is kept
		{"work", "", "🤖", false, "trusted", false, "work 🤖 🛡"},
		{"work", "", "", false, "trusted", false, "work 🛡"},
		{"work", "💼", "🤖", true, "trusted", false, "💼 🤖"},
		{"work", "💼", "🤖", false, "untrusted", false, "💼 🤖 ⚠"},
		{"work", "💼", "🤖", false, "denied", false, "💼 🤖 🚫"},
		// ESC-only icon sanitizes to "" — falls back to name
		{"work", "\x1b", "", false, "trusted", false, "work 🛡"},
		// ANSI sequence: ESC stripped, remaining "[2J" is safe printable text
		{"work", "", "\x1b[2J", false, "trusted", false, "work [2J 🛡"},
		// compact mode: no spaces between segments
		{"work", "💼", "🤖", false, "trusted", true, "💼🤖🛡"},
		{"work", "", "🤖", false, "trusted", true, "work🤖🛡"},
		{"work", "💼", "🤖", false, "untrusted", true, "💼🤖⚠"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := formatPromptLine(tt.ctx, tt.ctxIcon, tt.agentIcon, tt.sbDisabled, tt.trust, tt.compact)
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
