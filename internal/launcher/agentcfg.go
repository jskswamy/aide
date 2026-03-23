// Package launcher orchestrates agent discovery, sandbox setup, and process execution.
package launcher

import (
	"path/filepath"

	"github.com/jskswamy/aide/pkg/seatbelt"
	"github.com/jskswamy/aide/pkg/seatbelt/modules"
)

// agentModuleResolvers maps agent base names to their seatbelt module factory.
var agentModuleResolvers = map[string]func() seatbelt.Module{
	"claude": modules.ClaudeAgent,
	"codex":  modules.CodexAgent,
	"aider":  modules.AiderAgent,
	"goose":  modules.GooseAgent,
	"amp":    modules.AmpAgent,
	"gemini": modules.GeminiAgent,
}

// ResolveAgentModule returns the seatbelt module for the named agent, or nil.
func ResolveAgentModule(agentName string) seatbelt.Module {
	base := filepath.Base(agentName)
	if factory, ok := agentModuleResolvers[base]; ok {
		return factory()
	}
	return nil
}
