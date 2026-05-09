package modules

import "github.com/jskswamy/aide/pkg/seatbelt"

// CodexAgent returns a module with OpenAI Codex agent sandbox rules.
func CodexAgent() seatbelt.Module {
	return NewSimpleAgent(AgentSpec{
		DisplayName:     "Codex Agent",
		SectionName:     "Codex",
		EnvKey:          "CODEX_HOME",
		HomeRelDefaults: []string{".codex"},
	})
}
