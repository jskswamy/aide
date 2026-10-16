package modules

import "github.com/jskswamy/aide/pkg/seatbelt"

// PiAgent returns a module with Pi CLI agent sandbox rules.
// PI_CODING_AGENT_DIR is a documented env var overriding Pi's whole
// config directory (default ~/.pi/agent, per `pi --help`'s environment
// variables section). The home-relative default is the parent ".pi"
// rather than ".pi/agent" itself, in case other ".pi/*" subdirectories
// exist that the env var doesn't cover. When PI_CODING_AGENT_DIR is set
// (every Pi profile launch), only that dir is granted: not ~/.pi, so one
// profile cannot reach the default login or another profile's auth.json.
//
// Verified directly (2026-10-14, pi 1.1.0): `pi install`, `pi list` and
// `pi mcp list` against a fresh $HOME wrote only under ~/.pi/agent.
func PiAgent() seatbelt.Module {
	return NewSimpleAgent(AgentSpec{
		DisplayName:     "Pi Agent",
		SectionName:     "Pi",
		EnvKey:          "PI_CODING_AGENT_DIR",
		HomeRelDefaults: []string{".pi"},
	})
}
