package modules

import "github.com/jskswamy/aide/pkg/seatbelt"

// GooseAgent returns a module with Goose agent sandbox rules.
func GooseAgent() seatbelt.Module {
	return NewSimpleAgent(AgentSpec{
		DisplayName:     "Goose Agent",
		SectionName:     "Goose",
		EnvKey:          "GOOSE_PATH_ROOT",
		HomeRelDefaults: []string{".config/goose", ".local/share/goose", ".local/state/goose"},
	})
}
