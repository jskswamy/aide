// Git integration module for macOS Seatbelt profiles.
//
// Rules ported from agent-safehouse by Eugene Goldin:
// https://github.com/eugene1g/agent-safehouse
// Source: profiles/50-integrations-core/git.sb

package modules

import "github.com/jskswamy/aide/pkg/seatbelt"

type gitIntegrationModule struct{}

// GitIntegration returns a module with Git configuration read-only sandbox rules.
func GitIntegration() seatbelt.Module { return &gitIntegrationModule{} }

func (m *gitIntegrationModule) Name() string { return "Git Integration" }

func (m *gitIntegrationModule) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	home := ctx.HomeDir

	return []seatbelt.Rule{
		seatbelt.Section("Git configuration (read-only)"),
		seatbelt.Raw(`(allow file-read*
    ` + seatbelt.HomePrefix(home, ".gitconfig") + `
    ` + seatbelt.HomePrefix(home, ".gitignore") + `
    ` + seatbelt.HomeSubpath(home, ".config/git") + `
    ` + seatbelt.HomeLiteral(home, ".gitattributes") + `
    ` + seatbelt.HomeLiteral(home, ".ssh") + `
    ` + seatbelt.HomeLiteral(home, ".ssh/config") + `
    ` + seatbelt.HomeLiteral(home, ".ssh/known_hosts") + `
)`),
	}
}
