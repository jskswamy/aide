package modules

import "github.com/jskswamy/aide/pkg/seatbelt"

// GeminiAgent returns a module with Gemini CLI agent sandbox rules.
func GeminiAgent() seatbelt.Module {
	return NewSimpleAgent(AgentSpec{
		DisplayName:     "Gemini Agent",
		SectionName:     "Gemini",
		EnvKey:          "GEMINI_HOME",
		HomeRelDefaults: []string{".gemini", ".config/gemini"},
	})
}
