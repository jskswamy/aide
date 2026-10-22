package modules

import "github.com/jskswamy/aide/pkg/seatbelt"

// OpenCodeAgent returns a module with OpenCode CLI agent sandbox
// rules. OPENCODE_CONFIG_DIR overrides only OpenCode's config
// directory (default ~/.config/opencode); its data (log, db, auth),
// state and cache dirs stay at their XDG defaults, so they are granted
// independently of the override.
func OpenCodeAgent() seatbelt.Module {
	return openCodeModule{
		config: NewSimpleAgent(AgentSpec{
			DisplayName:     "OpenCode Agent",
			SectionName:     "OpenCode",
			EnvKey:          "OPENCODE_CONFIG_DIR",
			HomeRelDefaults: []string{".config/opencode"},
		}),
		state: NewSimpleAgent(AgentSpec{
			DisplayName: "OpenCode Agent",
			SectionName: "OpenCode data",
			HomeRelDefaults: []string{
				".local/share/opencode",
				".local/state/opencode",
				".cache/opencode",
			},
		}),
	}
}

type openCodeModule struct{ config, state seatbelt.Module }

func (m openCodeModule) Name() string { return m.config.Name() }

func (m openCodeModule) Rules(ctx *seatbelt.Context) seatbelt.GuardResult {
	r, s := m.config.Rules(ctx), m.state.Rules(ctx)
	r.Rules = append(r.Rules, s.Rules...)
	r.Writable = append(r.Writable, s.Writable...)
	r.Readable = append(r.Readable, s.Readable...)
	r.Skipped = append(r.Skipped, s.Skipped...)
	r.Hints = append(r.Hints, s.Hints...)
	return r
}
