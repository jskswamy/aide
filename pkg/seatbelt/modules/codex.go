package modules

import (
	"path/filepath"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

type codexAgentModule struct{}

// CodexAgent returns a module with OpenAI Codex agent sandbox rules.
func CodexAgent() seatbelt.Module { return &codexAgentModule{} }

func (m *codexAgentModule) Name() string { return "Codex Agent" }

func (m *codexAgentModule) Rules(ctx *seatbelt.Context) seatbelt.GuardResult {
	dirs := resolveConfigDirs(ctx, "CODEX_HOME", []string{
		filepath.Join(ctx.HomeDir, ".codex"),
	})
	return seatbelt.GuardResult{Rules: configDirRules("Codex", dirs)}
}
