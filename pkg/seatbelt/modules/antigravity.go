package modules

import "github.com/jskswamy/aide/pkg/seatbelt"

// AntigravityAgent returns a module with Google Antigravity CLI (`agy`) agent
// sandbox rules. Antigravity stores its configuration, CLI sessions, and
// customizations under ~/.gemini (including ~/.gemini/antigravity-cli and
// ~/.gemini/config).
func AntigravityAgent() seatbelt.Module {
	return NewSimpleAgent(AgentSpec{
		DisplayName:     "Antigravity Agent",
		SectionName:     "Antigravity",
		HomeRelDefaults: []string{".gemini", ".config/gemini"},
	})
}
