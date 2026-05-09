package modules

import "github.com/jskswamy/aide/pkg/seatbelt"

// AiderAgent returns a module with Aider agent sandbox rules.
func AiderAgent() seatbelt.Module {
	return NewSimpleAgent(AgentSpec{
		DisplayName:     "Aider Agent",
		SectionName:     "Aider",
		HomeRelDefaults: []string{".aider"},
	})
}
