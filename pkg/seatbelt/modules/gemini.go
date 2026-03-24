package modules

import (
	"path/filepath"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

type geminiAgentModule struct{}

// GeminiAgent returns a module with Gemini CLI agent sandbox rules.
func GeminiAgent() seatbelt.Module { return &geminiAgentModule{} }

func (m *geminiAgentModule) Name() string { return "Gemini Agent" }

func (m *geminiAgentModule) Rules(ctx *seatbelt.Context) seatbelt.GuardResult {
	dirs := resolveConfigDirs(ctx, "GEMINI_HOME", []string{
		filepath.Join(ctx.HomeDir, ".gemini"),
		filepath.Join(ctx.HomeDir, ".config", "gemini"),
	})
	return seatbelt.GuardResult{Rules: configDirRules("Gemini", dirs)}
}
