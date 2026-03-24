// Git integration guard for macOS Seatbelt profiles.
//
// Rules ported from agent-safehouse by Eugene Goldin:
// https://github.com/eugene1g/agent-safehouse
// Source: profiles/50-integrations-core/git.sb

package guards

import "github.com/jskswamy/aide/pkg/seatbelt"

type gitIntegrationGuard struct{}

// GitIntegrationGuard returns a Guard with Git configuration read-only sandbox rules.
func GitIntegrationGuard() seatbelt.Guard { return &gitIntegrationGuard{} }

func (g *gitIntegrationGuard) Name() string        { return "git-integration" }
func (g *gitIntegrationGuard) Type() string        { return "always" }
func (g *gitIntegrationGuard) Description() string {
	return "Git config and SSH host verification (read-only)"
}

func (g *gitIntegrationGuard) Rules(ctx *seatbelt.Context) seatbelt.GuardResult {
	// All git config reads are now covered by the filesystem guard's
	// scoped $HOME reads (.gitconfig dotfile, ~/.config/git/, ~/.ssh/).
	return seatbelt.GuardResult{}
}
