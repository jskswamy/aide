// Package launcher orchestrates agent discovery, sandbox setup, and process execution.
package launcher

import (
	"path/filepath"

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
}

// ResolveAgentModule returns the seatbelt module for the named agent, or nil.
func ResolveAgentModule(agentName string) seatbelt.Module {
	base := filepath.Base(agentName)
	if factory, ok := agentModuleResolvers[base]; ok {
		return factory()
	}
	return nil
}
