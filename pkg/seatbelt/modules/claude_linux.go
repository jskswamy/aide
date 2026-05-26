//go:build linux

package modules

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

var (
	_ seatbelt.EnvProvider    = (*claudeAgentModule)(nil)
	_ seatbelt.EnvKeyProvider = (*claudeAgentModule)(nil)
)

// claudeConfigDir returns the directory Claude should treat as its
// CLAUDE_CONFIG_DIR for the duration of the sandbox launch. Two cases:
//
//   - The user has CLAUDE_CONFIG_DIR set in their environment → use that
//     path (whatever they chose, aide does not second-guess).
//   - Unset → aide's default, ~/.config/aide/claude/.
//
// The Linux backend uses the result both for Landlock's writable allow-list
// (augmentLinuxPaths) and for the env-var injection (AgentEnv), so the two
// can never disagree about where Claude is reading and writing.
func claudeConfigDir(ctx *seatbelt.Context) string {
	if v, ok := ctx.EnvLookup("CLAUDE_CONFIG_DIR"); ok && v != "" {
		return v
	}
	return filepath.Join(ctx.HomeDir, ".config", "aide", "claude")
}

// AgentEnv injects CLAUDE_CONFIG_DIR pointing at aide's default redirect
// path, but only when the user has not set CLAUDE_CONFIG_DIR themselves.
// Respecting the user's choice keeps aide out of the way of users who
// already have a sandbox-friendly Claude layout (e.g. CI runners with a
// dedicated state dir, or per-project overrides).
//
// On macOS this method is a no-op via the build-tagged stub in
// claude_other.go — the Seatbelt profile still uses Claude's default
// $HOME paths until cross-platform unification lands.
func (m *claudeAgentModule) AgentEnv(ctx *seatbelt.Context) map[string]string {
	if ctx == nil || ctx.HomeDir == "" {
		return nil
	}
	if _, userSet := ctx.EnvLookup("CLAUDE_CONFIG_DIR"); userSet {
		return nil
	}
	dir := claudeConfigDir(ctx)
	// Landlock grants access on a path but cannot create the path itself
	// (creation needs write on the parent, which we don't grant). aide
	// ensures the aide-managed default exists before the sandbox is applied.
	// If MkdirAll fails, do not inject CLAUDE_CONFIG_DIR: pointing the agent
	// at an inaccessible directory would cause every Claude write to be denied
	// by Landlock with no user-visible diagnostic.
	if err := os.MkdirAll(dir, 0o700); err != nil {
		fmt.Fprintf(os.Stderr, "aide: warning: cannot create Claude config dir %q: %v; CLAUDE_CONFIG_DIR not injected\n", dir, err)
		return nil
	}
	return map[string]string{
		"CLAUDE_CONFIG_DIR": dir,
	}
}

// AgentEnvKeys returns the env key names this module may inject, without any
// filesystem side effects. agentEnvKeys in sandbox.go calls this instead of
// probing AgentEnv (which triggers os.MkdirAll) to safely discover which keys
// filterEnv must preserve under CleanEnv mode.
func (m *claudeAgentModule) AgentEnvKeys(_ *seatbelt.Context) []string {
	return []string{"CLAUDE_CONFIG_DIR"}
}

// augmentLinuxPaths populates GuardResult with the single Landlock-granted
// writable path: Claude's CLAUDE_CONFIG_DIR. Claude relocates every piece of
// its state (the atomic-renamed .claude.json, settings.json, .credentials.json,
// projects/, plugins/, cache/, backups/, session-env/) inside that one
// directory — verified by strace under Claude Code CLI ≥ 2.x. Granting one
// path replaces the previous hardcoded list and eliminates the need for the
// atomic-rename overlay entirely.
func augmentLinuxPaths(ctx *seatbelt.Context, result *seatbelt.GuardResult) {
	if ctx == nil || ctx.HomeDir == "" {
		return
	}
	result.Writable = append(result.Writable, claudeConfigDir(ctx))
}
