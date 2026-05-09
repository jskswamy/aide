package modules

import "github.com/jskswamy/aide/pkg/seatbelt"

// AmpAgent returns a module with Amp agent sandbox rules.
func AmpAgent() seatbelt.Module {
	return NewSimpleAgent(AgentSpec{
		DisplayName:     "Amp Agent",
		SectionName:     "Amp",
		EnvKey:          "AMP_HOME",
		HomeRelDefaults: []string{".amp", ".config/amp"},
	})
}
