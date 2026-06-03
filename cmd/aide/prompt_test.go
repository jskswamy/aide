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
		want                    string
	}{
		{"work", "💼", "🤖", false, "trusted", "work 💼 🤖 🛡"},
		{"work", "💼", "", false, "trusted", "work 💼 🛡"},
		{"work", "", "🤖", false, "trusted", "work 🤖 🛡"},
		{"work", "", "", false, "trusted", "work 🛡"},
		{"work", "💼", "🤖", true, "trusted", "work 💼 🤖"},
		{"work", "💼", "🤖", false, "untrusted", "work 💼 🤖 ⚠"},
		{"work", "💼", "🤖", false, "denied", "work 💼 🤖 🚫"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := formatPromptLine(tt.ctx, tt.ctxIcon, tt.agentIcon, tt.sbDisabled, tt.trust)
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
