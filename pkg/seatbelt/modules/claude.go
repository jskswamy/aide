// Package modules provides composable Seatbelt profile building blocks.
//
// Claude agent module rules ported from agent-safehouse by Eugene Goldin:
// https://github.com/eugene1g/agent-safehouse
// Source: profiles/60-agents/claude-code.sb
package modules

import (
	"path/filepath"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

type claudeAgentModule struct{}

// ClaudeAgent returns a module with Claude Code agent sandbox rules.
func ClaudeAgent() seatbelt.Module { return &claudeAgentModule{} }

func (m *claudeAgentModule) Name() string { return "Claude Agent" }

func (m *claudeAgentModule) Rules(ctx *seatbelt.Context) seatbelt.GuardResult {
	home := ctx.HomeDir

	// Config directories respect CLAUDE_CONFIG_DIR env var override.
	// When set, only the override path is allowed. When unset, all defaults.
	configDirs := resolveConfigDirs(ctx, "CLAUDE_CONFIG_DIR", []string{
		filepath.Join(home, ".claude"),
		filepath.Join(home, ".config", "claude"),
		filepath.Join(home, "Library", "Application Support", "Claude"),
	})

	rules := configDirRules("Claude", configDirs)

	// Runtime data paths: always present regardless of CLAUDE_CONFIG_DIR.
	rules = append(rules,
		seatbelt.SectionAllow("Claude user data"),
		seatbelt.AllowRule(`(allow file-read* file-write*
    `+seatbelt.HomePrefix(home, ".local/bin/claude")+`
    `+seatbelt.HomeSubpath(home, ".cache/claude")+`
    `+seatbelt.HomePrefix(home, ".claude.json")+`
    `+seatbelt.HomeLiteral(home, ".claude.lock")+`
    `+seatbelt.HomeSubpath(home, ".local/state/claude")+`
    `+seatbelt.HomeSubpath(home, ".local/share/claude")+`
    `+seatbelt.HomeLiteral(home, ".mcp.json")+`
)`),

		// Claude managed configuration (read-only)
		seatbelt.SectionAllow("Claude managed configuration"),
		seatbelt.AllowRule(`(allow file-read*
    `+seatbelt.HomePrefix(home, ".claude.json.")+`
    `+seatbelt.HomeLiteral(home, "Library/Application Support/Claude/claude_desktop_config.json")+`
    (subpath "/Library/Application Support/ClaudeCode/.claude")
    (literal "/Library/Application Support/ClaudeCode/managed-settings.json")
    (literal "/Library/Application Support/ClaudeCode/managed-mcp.json")
    (literal "/Library/Application Support/ClaudeCode/CLAUDE.md")
)`),
	)

	return seatbelt.GuardResult{Rules: rules}
}
