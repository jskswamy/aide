package modules

import (
	"strings"
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

func TestAgentModules(t *testing.T) {
	tests := []struct {
		name        string
		module      seatbelt.Module
		wantName    string
		env         []string
		wantContain []string
		wantAbsent  []string
	}{
		{
			name:     "Codex defaults",
			module:   CodexAgent(),
			wantName: "Codex Agent",
			wantContain: []string{
				"/home/user/.codex",
			},
		},
		{
			name:     "Codex env override",
			module:   CodexAgent(),
			wantName: "Codex Agent",
			env:      []string{"CODEX_HOME=/custom/codex"},
			wantContain: []string{
				"/custom/codex",
			},
			wantAbsent: []string{
				"/home/user/.codex",
			},
		},
		{
			name:     "Aider defaults",
			module:   AiderAgent(),
			wantName: "Aider Agent",
			wantContain: []string{
				"/home/user/.aider",
			},
		},
		{
			name:     "Goose defaults",
			module:   GooseAgent(),
			wantName: "Goose Agent",
			wantContain: []string{
				"/home/user/.config/goose",
				"/home/user/.local/share/goose",
				"/home/user/.local/state/goose",
			},
		},
		{
			name:     "Goose env override",
			module:   GooseAgent(),
			wantName: "Goose Agent",
			env:      []string{"GOOSE_PATH_ROOT=/custom/goose"},
			wantContain: []string{
				"/custom/goose",
			},
			wantAbsent: []string{
				"/home/user/.config/goose",
				"/home/user/.local/share/goose",
			},
		},
		{
			name:     "Amp defaults",
			module:   AmpAgent(),
			wantName: "Amp Agent",
			wantContain: []string{
				"/home/user/.amp",
				"/home/user/.config/amp",
			},
		},
		{
			name:     "Amp env override",
			module:   AmpAgent(),
			wantName: "Amp Agent",
			env:      []string{"AMP_HOME=/custom/amp"},
			wantContain: []string{
				"/custom/amp",
			},
			wantAbsent: []string{
				"/home/user/.amp",
				"/home/user/.config/amp",
			},
		},
		{
			name:     "Gemini defaults",
			module:   GeminiAgent(),
			wantName: "Gemini Agent",
			wantContain: []string{
				"/home/user/.gemini",
				"/home/user/.config/gemini",
			},
		},
		{
			name:     "Gemini env override",
			module:   GeminiAgent(),
			wantName: "Gemini Agent",
			env:      []string{"GEMINI_HOME=/custom/gemini"},
			wantContain: []string{
				"/custom/gemini",
			},
			wantAbsent: []string{
				"/home/user/.gemini",
				"/home/user/.config/gemini",
			},
		},
		{
			name:     "Copilot defaults",
			module:   CopilotAgent(),
			wantName: "Copilot Agent",
			wantContain: []string{
				"/home/user/.copilot",
				"/home/user/.config/.copilot",
				"/home/user/.config/copilot",
				"/home/user/.local/state/.copilot",
				"/home/user/.local/state/copilot",
			},
		},
		{
			name:     "Copilot env override",
			module:   CopilotAgent(),
			wantName: "Copilot Agent",
			env:      []string{"COPILOT_HOME=/custom/copilot"},
			wantContain: []string{
				"/custom/copilot",
			},
			wantAbsent: []string{
				"/home/user/.copilot",
				"/home/user/.config/.copilot",
				"/home/user/.config/copilot",
			},
		},
		{
			name:     "Copilot XDG override",
			module:   CopilotAgent(),
			wantName: "Copilot Agent",
			env:      []string{"XDG_CONFIG_HOME=/custom/xdg", "XDG_STATE_HOME=/custom/state"},
			wantContain: []string{
				"/home/user/.copilot",
				"/custom/xdg/.copilot",
				"/custom/xdg/copilot",
				"/custom/state/.copilot",
				"/custom/state/copilot",
			},
			wantAbsent: []string{
				"/home/user/.config/.copilot",
				"/home/user/.config/copilot",
				"/home/user/.local/state/.copilot",
				"/home/user/.local/state/copilot",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.module.Name(); got != tt.wantName {
				t.Errorf("Name() = %q, want %q", got, tt.wantName)
			}

			ctx := &seatbelt.Context{
				HomeDir: "/home/user",
				Env:     tt.env,
			}
			result := tt.module.Rules(ctx)
			got := rulesToString(result.Rules)

			for _, s := range tt.wantContain {
				if !strings.Contains(got, s) {
					t.Errorf("expected %q in rules, got:\n%s", s, got)
				}
			}
			for _, s := range tt.wantAbsent {
				if strings.Contains(got, s) {
					t.Errorf("expected %q to be absent from rules, got:\n%s", s, got)
				}
			}
		})
	}
}
