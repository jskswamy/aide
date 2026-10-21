package modules

import "github.com/jskswamy/aide/pkg/seatbelt"

// OpenCodeAgent returns a module with OpenCode CLI agent sandbox
// rules. OPENCODE_CONFIG_DIR is an env var overriding OpenCode's config
// directory (default ~/.config/opencode).
func OpenCodeAgent() seatbelt.Module {
	return NewSimpleAgent(AgentSpec{
		DisplayName: "OpenCode Agent",
		SectionName: "OpenCode",
		EnvKey:      "OPENCODE_CONFIG_DIR",
		HomeRelDefaults: []string{
			".config/opencode",
			".local/share/opencode",
			".local/state/opencode",
			".cache/opencode",
		},
	})
}
