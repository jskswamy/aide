// Package launcher orchestrates agent discovery, sandbox setup, and process execution.
package launcher

import (
	"path/filepath"
	"runtime"

	"github.com/jskswamy/aide/pkg/seatbelt"
	"github.com/jskswamy/aide/pkg/seatbelt/modules"
)

// agentModuleResolvers maps agent base names to their seatbelt module factory.
var agentModuleResolvers = map[string]func() seatbelt.Module{
	"aider":        modules.AiderAgent,
	"amp":          modules.AmpAgent,
	"claude":       modules.ClaudeAgent,
	"codex":        modules.CodexAgent,
	"copilot":      modules.CopilotAgent,
	"cursor-agent": modules.CursorAgent,
	"gemini":       modules.GeminiAgent,
	"goose":        modules.GooseAgent,
	"opencode":     modules.OpenCodeAgent,
	"pi":           modules.PiAgent,
}

// ResolveAgentModule returns the seatbelt module for the named agent, or nil.
func ResolveAgentModule(agentName string) seatbelt.Module {
	base := filepath.Base(agentName)
	if factory, ok := agentModuleResolvers[base]; ok {
		return factory()
	}
	return nil
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
