package modules

import (
	"path/filepath"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

type ampAgentModule struct{}

// AmpAgent returns a module with Amp agent sandbox rules.
func AmpAgent() seatbelt.Module { return &ampAgentModule{} }

func (m *ampAgentModule) Name() string { return "Amp Agent" }

func (m *ampAgentModule) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	dirs := resolveConfigDirs(ctx, "AMP_HOME", []string{
		filepath.Join(ctx.HomeDir, ".amp"),
		filepath.Join(ctx.HomeDir, ".config", "amp"),
	})
	return configDirRules("Amp", dirs)
}
