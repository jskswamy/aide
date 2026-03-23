package modules

import (
	"path/filepath"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

type aiderAgentModule struct{}

// AiderAgent returns a module with Aider agent sandbox rules.
func AiderAgent() seatbelt.Module { return &aiderAgentModule{} }

func (m *aiderAgentModule) Name() string { return "Aider Agent" }

func (m *aiderAgentModule) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	// Aider has no single config dir env var override.
	dirs := resolveConfigDirs(ctx, "", []string{
		filepath.Join(ctx.HomeDir, ".aider"),
	})
	return configDirRules("Aider", dirs)
}
