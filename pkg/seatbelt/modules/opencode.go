package modules

import (
	"os"
	"path/filepath"
	"slices"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

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

// openCodeProbedNames are the names OpenCode's server resolves in the
// home dir and in every ancestor of the project at startup (after
// resolving each ancestor itself). Opening an existing one that is denied makes the server answer 500, so the TUI
// never starts; a missing one is fine.
var openCodeProbedNames = []string{".claude", ".agents", ".opencode", "opencode.json", "opencode.jsonc"}

// openCodeProbedPaths returns the paths the server must be able to open:
// the home dir and each ancestor of projectRoot themselves, plus the
// existing probed names inside each.
func openCodeProbedPaths(home, projectRoot string) []string {
	var dirs []string
	for _, d := range []string{home, projectRoot} {
		for ; d != "" && d != "." && !slices.Contains(dirs, d); d = filepath.Dir(d) {
			dirs = append(dirs, d)
			if d == filepath.Dir(d) {
				break
			}
		}
	}
	out := slices.Clone(dirs)
	for _, d := range dirs {
		for _, name := range openCodeProbedNames {
			p := filepath.Join(d, name)
			if _, err := os.Lstat(p); err == nil {
				out = append(out, p)
			}
		}
	}
	return out
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
	r.Rules = append(r.Rules, seatbelt.SectionAllow("OpenCode skill discovery"))
	// Literal grants only: names become visible, contents stay unreadable.
	for _, p := range openCodeProbedPaths(ctx.HomeDir, ctx.ProjectRoot) {
		r.Rules = append(r.Rules, seatbelt.AllowLiteral(p, "file-read-data"))
	}
	return r
}
