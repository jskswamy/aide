package modules

import "github.com/jskswamy/aide/pkg/seatbelt"

// OpenCodeAgent returns a module with OpenCode CLI agent sandbox
// rules. OpenCode has no documented env var that redirects its whole
// config/data/cache home the way CLAUDE_CONFIG_DIR or GEMINI_HOME do
// (only OPENCODE_CONFIG, which points at a single file, not a
// directory) — EnvKey is left empty, matching how AgentSpec treats an
// empty EnvKey as "no override mechanism".
//
// Home-relative defaults verified directly (2026-08-31, opencode
// 1.18.18 via nix-shell): a single `opencode mcp add` invocation
// against a fresh $HOME populated all four directories below.
func OpenCodeAgent() seatbelt.Module {
	return NewSimpleAgent(AgentSpec{
		DisplayName: "OpenCode Agent",
		SectionName: "OpenCode",
		HomeRelDefaults: []string{
			".config/opencode",
			".local/share/opencode",
			".local/state/opencode",
			".cache/opencode",
		},
	})
}
