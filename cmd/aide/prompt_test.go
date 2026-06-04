package main

import (
	"strings"
	"testing"
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
