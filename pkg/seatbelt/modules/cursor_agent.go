package modules

import (
	"fmt"
	"os/exec"
	"path/filepath"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

// installDirResolver returns ("", "", false) when cursor-agent is absent or
// its path cannot be resolved. Injected to keep install-dir branches
// reachable in CI without cursor-agent on PATH.
type installDirResolver func(home string) (activeVerDir, logsDir string, ok bool)

type cursorAgentModule struct {
	resolveInstallDirs installDirResolver
}

// CursorAgent returns a module with Cursor CLI (cursor-agent) sandbox rules.
func CursorAgent() seatbelt.Module {
	return &cursorAgentModule{resolveInstallDirs: cursorActiveInstallDirs}
}

func (m *cursorAgentModule) Name() string { return "Cursor Agent" }

func (m *cursorAgentModule) Rules(ctx *seatbelt.Context) seatbelt.GuardResult {
	if ctx == nil || ctx.HomeDir == "" {
		return seatbelt.GuardResult{}
	}
	home := ctx.HomeDir

	rules := configDirRules("Cursor", cursorConfigDirs(ctx))

	activeVerDir, logsDir, ok := m.resolveInstallDirs(home)
	if ok {
		rules = append(rules,
			seatbelt.SectionAllow("Cursor install"),
			seatbelt.AllowRule(fmt.Sprintf("(allow file-read* file-write*\n    (subpath %q)\n)", logsDir)),
			seatbelt.AllowRule(fmt.Sprintf("(allow file-read*\n    (subpath %q)\n)", activeVerDir)),
		)
	}

	return seatbelt.GuardResult{Rules: rules}
}

// cursorConfigDirs returns ~/.cursor and the XDG cursor config directory.
// CURSOR_CONFIG_DIR, when set, overrides both. XDG_CONFIG_HOME/cursor (or its
// XDG default $HOME/.config/cursor when unset) is always appended; cursor-agent
// stores auth.json there on Linux.
func cursorConfigDirs(ctx *seatbelt.Context) []string {
	home := ctx.HomeDir

	if dir, ok := ctx.EnvLookup("CURSOR_CONFIG_DIR"); ok && dir != "" {
		return []string{dir}
	}

	xdgBase := filepath.Join(home, ".config")
	if xdg, ok := ctx.EnvLookup("XDG_CONFIG_HOME"); ok && xdg != "" {
		xdgBase = xdg
	}
	return []string{
		filepath.Join(home, ".cursor"),
		filepath.Join(xdgBase, "cursor"),
	}
}

func cursorActiveInstallDirs(home string) (activeVerDir, logsDir string, ok bool) {
	binary, err := exec.LookPath("cursor-agent")
	if err != nil {
		return "", "", false
	}
	resolved, err := filepath.EvalSymlinks(binary)
	if err != nil {
		return "", "", false
	}
	return deriveCursorInstallDirs(resolved, home)
}

// deriveCursorInstallDirs derives (activeVerDir, logsDir) from a resolved
// cursor-agent binary path. The on-disk layout (same on Linux and macOS) is:
//
//	~/.local/share/cursor-agent/versions/<ver>/cursor-agent  (the binary)
//	~/.local/share/cursor-agent/logs                         (logs sibling)
//
// so logs is two parents up from the binary, then "logs".
func deriveCursorInstallDirs(resolvedBinary, _ string) (activeVerDir, logsDir string, ok bool) {
	activeVerDir = filepath.Dir(resolvedBinary)
	logsDir = filepath.Clean(filepath.Join(activeVerDir, "..", "..", "logs"))
	return activeVerDir, logsDir, true
}
