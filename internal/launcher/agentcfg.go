// Package launcher orchestrates agent discovery, sandbox setup, and process execution.
package launcher

import (
	"path/filepath"
	"runtime"

	"github.com/jskswamy/aide/internal/harness"
	"github.com/jskswamy/aide/pkg/seatbelt"
)

// ResolveAgentModule returns the seatbelt module for the named agent, or nil.
// The agent is matched by binary base name against canonical harness
// names only; aliases never select a sandbox module.
func ResolveAgentModule(agentName string) seatbelt.Module {
	d, ok := launchableHarness(agentName)
	if !ok {
		return nil
	}
	return d.Sandbox()
}

// launchableHarness returns the launchable harness whose canonical name
// is agentName's base name.
func launchableHarness(agentName string) (harness.Descriptor, bool) {
	base := filepath.Base(agentName)
	d, ok := harness.Lookup(base)
	if !ok || d.Name != base || !d.Launchable() {
		return harness.Descriptor{}, false
	}
	return d, true
}

// ApplyLaunchArgs returns extraArgs with the agent's always-on launch
// flags applied (see harness.Descriptor.ApplyLaunchArgs).
func ApplyLaunchArgs(agentName string, extraArgs []string) []string {
	if d, ok := launchableHarness(agentName); ok {
		return d.ApplyLaunchArgs(extraArgs)
	}
	return extraArgs
}

// SandboxAgentEnv returns the env overrides the named agent's sandbox
// module injects at launch (e.g. Claude's CLAUDE_CONFIG_DIR redirect to
// ~/.config/aide/claude on Linux). Provisioning that runs outside a
// launch (aide sync, agent permission grants) applies the same overrides
// so it writes the config dir the sandboxed agent will actually read.
// env is the full KEY=VALUE environment the module inspects for user
// overrides. Returns nil when the agent has no module or the module
// injects nothing.
func SandboxAgentEnv(agentName, homeDir string, env []string) map[string]string {
	provider, ok := ResolveAgentModule(agentName).(seatbelt.EnvProvider)
	if !ok {
		return nil
	}
	return provider.AgentEnv(&seatbelt.Context{HomeDir: homeDir, Env: env, GOOS: runtime.GOOS})
}
