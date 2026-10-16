// Package modules provides composable Seatbelt profile building blocks.
//
// Claude agent module rules ported from agent-safehouse by Eugene Goldin:
// https://github.com/eugene1g/agent-safehouse
// Source: profiles/60-agents/claude-code.sb
package modules

import (
	"path/filepath"

	"github.com/jskswamy/aide/internal/homepath"
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

	rules := configDirRules("Claude", home, configDirs)

	// Install, update and cache data: shared by every profile, holds no
	// login state. ~/.cache/claude is also inside the filesystem guard's
	// ~/.cache grant.
	rules = append(rules,
		seatbelt.SectionAllow("Claude user data"),
		seatbelt.AllowRule(`(allow file-read* file-write*
    `+seatbelt.HomePrefix(home, ".local/bin/claude")+`
    `+seatbelt.HomeSubpath(home, ".cache/claude")+`
    `+seatbelt.HomeSubpath(home, ".local/state/claude")+`
    `+seatbelt.HomeSubpath(home, ".local/share/claude")+`
    `+seatbelt.HomeLiteral(home, ".mcp.json")+`
)`),
	)
	rules = append(rules, claudeAccountStateRules(ctx)...)

	// Claude managed configuration (read-only)
	rules = append(rules,
		seatbelt.SectionAllow("Claude managed configuration"),
		seatbelt.AllowRule(`(allow file-read*
    `+seatbelt.HomeLiteral(home, "Library/Application Support/Claude/claude_desktop_config.json")+`
    (subpath "/Library/Application Support/ClaudeCode/.claude")
    (literal "/Library/Application Support/ClaudeCode/managed-settings.json")
    (literal "/Library/Application Support/ClaudeCode/managed-mcp.json")
    (literal "/Library/Application Support/ClaudeCode/CLAUDE.md")
)`),
	)

	result := seatbelt.GuardResult{Rules: rules}
	augmentLinuxPaths(ctx, &result)
	return result
}

// claudeAccountStateRules grants the account state Claude keeps beside
// its config dir. Without CLAUDE_CONFIG_DIR that is ~/.claude.json (its
// lock and backups) and ~/.claude.lock. With it, Claude keeps
// .claude.json inside the dir (already granted) and never reads the
// global one, so a profile launch gets only its own dir's sibling lock
// (<dir>.lock): the global file would expose another login's account
// and project state.
func claudeAccountStateRules(ctx *seatbelt.Context) []seatbelt.Rule {
	home := ctx.HomeDir
	if dir, ok := ctx.EnvLookup("CLAUDE_CONFIG_DIR"); ok && dir != "" {
		lock := filepath.Clean(homepath.Expand(dir, home)) + ".lock"
		return []seatbelt.Rule{
			seatbelt.SectionAllow("Claude config dir lock"),
			seatbelt.AllowLiteral(lock, "file-read*", "file-write*"),
		}
	}
	return []seatbelt.Rule{
		seatbelt.SectionAllow("Claude account state"),
		seatbelt.AllowRule(`(allow file-read* file-write*
    ` + seatbelt.HomePrefix(home, ".claude.json") + `
    ` + seatbelt.HomeLiteral(home, ".claude.lock") + `
)`),
	}
}
