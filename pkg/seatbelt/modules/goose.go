package modules

import (
	"path/filepath"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

type gooseAgentModule struct{}

// GooseAgent returns a module with Goose agent sandbox rules.
func GooseAgent() seatbelt.Module { return &gooseAgentModule{} }

func (m *gooseAgentModule) Name() string { return "Goose Agent" }

func (m *gooseAgentModule) Rules(ctx *seatbelt.Context) seatbelt.GuardResult {
	dirs := resolveConfigDirs(ctx, "GOOSE_PATH_ROOT", []string{
		filepath.Join(ctx.HomeDir, ".config", "goose"),
		filepath.Join(ctx.HomeDir, ".local", "share", "goose"),
		filepath.Join(ctx.HomeDir, ".local", "state", "goose"),
	})
	return seatbelt.GuardResult{Rules: configDirRules("Goose", dirs)}
}
