package modules

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

// trustedInstallPrefixes are the only locations a cursor-agent binary may
// resolve to. Anything outside is rejected to block malicious PATH entries.
//
// The Cursor Agent CLI installer (https://cursor.com/install) uses the same
// layout on both Linux and macOS:
//
//	~/.local/share/cursor-agent/versions/<ver>/cursor-agent  (the binary)
//	~/.local/share/cursor-agent/logs                         (logs sibling)
//	~/.local/bin/cursor-agent  ->  ../share/cursor-agent/versions/<ver>/cursor-agent
//
// The Cursor desktop IDE bundle (/Applications/Cursor.app) does not ship the
// standalone CLI, so it is intentionally not listed here.
func trustedInstallPrefixes(home string) []string {
	return []string{
		filepath.Join(home, ".local", "share", "cursor-agent", "versions") + string(filepath.Separator),
	}
}

// installDirResolver returns ("", "", false) when cursor-agent is absent or
// its path cannot be resolved or fails the trusted-prefix check. Injected to
// keep install-dir branches reachable in CI without cursor-agent on PATH.
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

	rules := configDirRules("Cursor", home, cursorConfigDirs(ctx))

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

// cursorConfigDirs resolves config directories for cursor-agent.
// CURSOR_CONFIG_DIR is accepted only when under $HOME and outside sensitive
// dirs; unsafe values fall back to defaults. XDG_CONFIG_HOME/cursor (or its
// XDG default $HOME/.config/cursor when unset) augments ~/.cursor.
func cursorConfigDirs(ctx *seatbelt.Context) []string {
	home := ctx.HomeDir

	xdgBase := filepath.Join(home, ".config")
	if xdg, ok := ctx.EnvLookup("XDG_CONFIG_HOME"); ok && xdg != "" {
		xdgBase = xdg
	}

	defaults := []string{filepath.Join(home, ".cursor")}
	return resolveConfigDirsAdditive(ctx, "CURSOR_CONFIG_DIR", filepath.Join(xdgBase, "cursor"), defaults)
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
// so logs is two parents up from the binary, then "logs". Paths outside the
// trusted-prefix list are rejected to block binaries on attacker-controlled
// PATH entries.
func deriveCursorInstallDirs(resolvedBinary, home string) (activeVerDir, logsDir string, ok bool) {
	dir := filepath.Dir(resolvedBinary)
	if !isTrustedInstallDir(dir, home) {
		return "", "", false
	}
	activeVerDir = dir
	logsDir = filepath.Clean(filepath.Join(activeVerDir, "..", "..", "logs"))
	return activeVerDir, logsDir, true
}

func isTrustedInstallDir(dir, home string) bool {
	// Append separator before comparing so .../versions/1.2 matches
	// .../versions/ without also matching .../versions-extra/.
	candidate := dir + string(filepath.Separator)
	for _, prefix := range trustedInstallPrefixes(home) {
		if strings.HasPrefix(candidate, prefix) {
			return true
		}
	}
	return false
}
