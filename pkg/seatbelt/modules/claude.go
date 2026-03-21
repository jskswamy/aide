// Claude agent module for macOS Seatbelt profiles.
//
// Rules ported from agent-safehouse by Eugene Goldin:
// https://github.com/eugene1g/agent-safehouse
// Source: profiles/60-agents/claude-code.sb

package modules

import "github.com/jskswamy/aide/pkg/seatbelt"

type claudeAgentModule struct{}

// ClaudeAgent returns a module with Claude Code agent sandbox rules.
func ClaudeAgent() seatbelt.Module { return &claudeAgentModule{} }

func (m *claudeAgentModule) Name() string { return "Claude Agent" }

func (m *claudeAgentModule) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	home := ctx.HomeDir

	return []seatbelt.Rule{
		// Claude user data (read-write)
		seatbelt.Section("Claude user data"),
		seatbelt.Raw(`(allow file-read* file-write*
    ` + seatbelt.HomePrefix(home, ".local/bin/claude") + `
    ` + seatbelt.HomeSubpath(home, ".cache/claude") + `
    ` + seatbelt.HomeSubpath(home, ".claude") + `
    ` + seatbelt.HomePrefix(home, ".claude.json") + `
    ` + seatbelt.HomeLiteral(home, ".claude.lock") + `
    ` + seatbelt.HomeSubpath(home, ".config/claude") + `
    ` + seatbelt.HomeSubpath(home, ".local/state/claude") + `
    ` + seatbelt.HomeSubpath(home, ".local/share/claude") + `
    ` + seatbelt.HomeLiteral(home, ".mcp.json") + `
)`),

		// Claude managed configuration (read-only)
		seatbelt.Section("Claude managed configuration"),
		seatbelt.Raw(`(allow file-read*
    ` + seatbelt.HomePrefix(home, ".claude.json.") + `
    ` + seatbelt.HomeLiteral(home, "Library/Application Support/Claude/claude_desktop_config.json") + `
    (subpath "/Library/Application Support/ClaudeCode/.claude")
    (literal "/Library/Application Support/ClaudeCode/managed-settings.json")
    (literal "/Library/Application Support/ClaudeCode/managed-mcp.json")
    (literal "/Library/Application Support/ClaudeCode/CLAUDE.md")
)`),
	}
}
