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

func (m *claudeAgentModule) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	home := ctx.HomeDir

	// Config directories respect CLAUDE_CONFIG_DIR env var override.
	configDirs := resolveConfigDirs(ctx, "CLAUDE_CONFIG_DIR", []string{
		filepath.Join(home, ".cache", "claude"),
		filepath.Join(home, ".claude"),
		filepath.Join(home, ".config", "claude"),
		filepath.Join(home, ".local", "state", "claude"),
		filepath.Join(home, ".local", "share", "claude"),
	})

	rules := configDirRules("Claude", configDirs)

	// Non-config paths: always present regardless of env override.
	rules = append(rules,
		seatbelt.SectionGrant("Claude user data"),
		seatbelt.GrantRule(`(allow file-read* file-write*
    `+seatbelt.HomePrefix(home, ".local/bin/claude")+`
    `+seatbelt.HomePrefix(home, ".claude.json")+`
    `+seatbelt.HomeLiteral(home, ".claude.lock")+`
    `+seatbelt.HomeLiteral(home, ".mcp.json")+`
)`),

		// Claude managed configuration (read-only)
		seatbelt.SectionGrant("Claude managed configuration"),
		seatbelt.GrantRule(`(allow file-read*
    `+seatbelt.HomePrefix(home, ".claude.json.")+`
    `+seatbelt.HomeLiteral(home, "Library/Application Support/Claude/claude_desktop_config.json")+`
    (subpath "/Library/Application Support/ClaudeCode/.claude")
    (literal "/Library/Application Support/ClaudeCode/managed-settings.json")
    (literal "/Library/Application Support/ClaudeCode/managed-mcp.json")
    (literal "/Library/Application Support/ClaudeCode/CLAUDE.md")
)`),
	)

	return rules
}
