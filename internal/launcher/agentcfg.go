package launcher

import (
	"os"
	"path/filepath"
	"strings"
)

// AgentConfigResolver returns directories an agent needs write access to,
// given the process environment. Returns nil if no special dirs are needed.
type AgentConfigResolver func(env []string, homeDir string) []string

// agentConfigResolvers maps agent base names to their config dir resolvers.
var agentConfigResolvers = map[string]AgentConfigResolver{
	"claude": claudeConfigDirs,
	"codex":  codexConfigDirs,
	"aider":  aiderConfigDirs,
	"goose":  gooseConfigDirs,
	"amp":    ampConfigDirs,
}

// ResolveAgentConfigDirs returns directories the named agent needs
// write access to, based on the process environment.
// Returns nil for unknown agents.
func ResolveAgentConfigDirs(agentName string, env []string, homeDir string) []string {
	base := filepath.Base(agentName)
	if resolver, ok := agentConfigResolvers[base]; ok {
		return resolver(env, homeDir)
	}
	return nil
}

// claudeConfigDirs returns Claude's config directories.
// Env override: CLAUDE_CONFIG_DIR
// Defaults: ~/.claude, ~/.config/claude, ~/Library/Application Support/Claude
func claudeConfigDirs(env []string, homeDir string) []string {
	if dir := envLookup(env, "CLAUDE_CONFIG_DIR"); dir != "" {
		return []string{dir}
	}
	return defaultDirs(homeDir,
		filepath.Join(homeDir, ".claude"),
		filepath.Join(homeDir, ".config", "claude"),
		filepath.Join(homeDir, "Library", "Application Support", "Claude"),
	)
}

// codexConfigDirs returns Codex's config directories.
// Env override: CODEX_HOME
// Default: ~/.codex
func codexConfigDirs(env []string, homeDir string) []string {
	if dir := envLookup(env, "CODEX_HOME"); dir != "" {
		return []string{dir}
	}
	return defaultDirs(homeDir, filepath.Join(homeDir, ".codex"))
}

// aiderConfigDirs returns Aider's config directories.
// No env override (aider uses per-option AIDER_* vars).
// Default: ~/.aider
func aiderConfigDirs(_ []string, homeDir string) []string {
	return defaultDirs(homeDir, filepath.Join(homeDir, ".aider"))
}

// gooseConfigDirs returns Goose's config directories.
// Env override: GOOSE_PATH_ROOT
// Defaults: ~/.config/goose, ~/.local/share/goose, ~/.local/state/goose
func gooseConfigDirs(env []string, homeDir string) []string {
	if dir := envLookup(env, "GOOSE_PATH_ROOT"); dir != "" {
		return []string{dir}
	}
	return defaultDirs(homeDir,
		filepath.Join(homeDir, ".config", "goose"),
		filepath.Join(homeDir, ".local", "share", "goose"),
		filepath.Join(homeDir, ".local", "state", "goose"),
	)
}

// ampConfigDirs returns Amp's config directories.
// Env override: AMP_HOME
// Defaults: ~/.amp, ~/.config/amp
func ampConfigDirs(env []string, homeDir string) []string {
	if dir := envLookup(env, "AMP_HOME"); dir != "" {
		return []string{dir}
	}
	return defaultDirs(homeDir,
		filepath.Join(homeDir, ".amp"),
		filepath.Join(homeDir, ".config", "amp"),
	)
}

// envLookup finds a key in a KEY=VALUE slice.
// Returns "" for missing keys. Treats explicitly empty (KEY=) as unset.
func envLookup(env []string, key string) string {
	prefix := key + "="
	for _, e := range env {
		if strings.HasPrefix(e, prefix) {
			val := e[len(prefix):]
			if val == "" {
				return "" // treat KEY= as unset
			}
			return val
		}
	}
	return ""
}

// defaultDirs returns candidates that exist on disk, plus any that
// don't exist yet but are under homeDir (agents create these on
// first run, so they must be writable from the start).
func defaultDirs(homeDir string, candidates ...string) []string {
	var dirs []string
	for _, p := range candidates {
		if _, err := os.Lstat(p); err == nil {
			dirs = append(dirs, p)
		} else if strings.HasPrefix(p, homeDir) {
			dirs = append(dirs, p)
		}
	}
	return dirs
}
