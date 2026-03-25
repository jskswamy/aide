package modules

import (
	"path/filepath"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

type copilotAgentModule struct{}

// CopilotAgent returns a module with GitHub Copilot CLI agent sandbox rules.
func CopilotAgent() seatbelt.Module { return &copilotAgentModule{} }

func (m *copilotAgentModule) Name() string { return "Copilot Agent" }

func (m *copilotAgentModule) Rules(ctx *seatbelt.Context) seatbelt.GuardResult {
	home := ctx.HomeDir

	// COPILOT_HOME overrides everything — only that path is used.
	if dir, ok := ctx.EnvLookup("COPILOT_HOME"); ok && dir != "" {
		return seatbelt.GuardResult{Rules: configDirRules("Copilot", []string{dir})}
	}

	// Default config directory.
	dirs := resolveConfigDirs(ctx, "", []string{
		filepath.Join(home, ".copilot"),
	})

	// Copilot CLI has a known bug (#1750) where it uses a dot-prefixed
	// directory under XDG paths (e.g. $XDG_CONFIG_HOME/.copilot instead
	// of $XDG_CONFIG_HOME/copilot). Include both buggy and correct paths
	// for forward compatibility.
	//
	// XDG dirs are appended directly (not through resolveConfigDirs)
	// because they may live outside $HOME when XDG vars are overridden.
	dirs = append(dirs, xdgCopilotDirs(ctx)...)

	return seatbelt.GuardResult{Rules: configDirRules("Copilot", dirs)}
}

// xdgCopilotDirs returns Copilot config/state directories under XDG base
// paths. Both the current buggy (.copilot) and future correct (copilot)
// subdirectories are included.
func xdgCopilotDirs(ctx *seatbelt.Context) []string {
	var dirs []string

	xdgConfig, _ := ctx.EnvLookup("XDG_CONFIG_HOME")
	if xdgConfig == "" {
		xdgConfig = filepath.Join(ctx.HomeDir, ".config")
	}
	dirs = append(dirs,
		filepath.Join(xdgConfig, ".copilot"),
		filepath.Join(xdgConfig, "copilot"),
	)

	xdgState, _ := ctx.EnvLookup("XDG_STATE_HOME")
	if xdgState == "" {
		xdgState = filepath.Join(ctx.HomeDir, ".local", "state")
	}
	dirs = append(dirs,
		filepath.Join(xdgState, ".copilot"),
		filepath.Join(xdgState, "copilot"),
	)

	return dirs
}
