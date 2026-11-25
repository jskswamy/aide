package sandbox

import (
	"fmt"
	"path/filepath"
	"slices"

	"github.com/jskswamy/aide/internal/capability"
	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/homepath"
	"github.com/jskswamy/aide/internal/pathguard"
)

// terminalBrowserGrants holds the dynamic sandbox grants resolved for the
// built-in terminal-browser capability.
type terminalBrowserGrants struct {
	Readable []string
	Writable []string
	Allow    []string
}

// resolveTerminalBrowserGrants computes validated readable, writable, and
// macOS Seatbelt Unix-socket Allow grants for terminal-browser.
func resolveTerminalBrowserGrants(cfg *config.Config, home string, getenv func(string) string, goos string) terminalBrowserGrants {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	checker := pathguard.NewChecker(cfg, home, getenv)
	var out terminalBrowserGrants
	var socketDirs []string

	addUnique := func(dst *[]string, items ...string) {
		for _, p := range items {
			if !slices.Contains(*dst, p) {
				*dst = append(*dst, p)
			}
		}
	}

	seatbeltSafe := func(paths []string) bool {
		if goos != "darwin" {
			return true
		}
		for _, p := range paths {
			if !pathguard.SafeSeatbeltPath(p) {
				return false
			}
		}
		return true
	}

	addWritable := func(dir string, hasSockets bool) {
		granted := checker.GrantDir(dir, true)
		if len(granted) == 0 || !seatbeltSafe(granted) {
			return
		}
		addUnique(&out.Writable, granted...)
		if hasSockets {
			// granted[0] is always the symlink-resolved path.
			addUnique(&socketDirs, granted[0])
		}
	}

	// 1. Hashed XDG data, state, and runtime directories.
	dataDirs, stateDirs, runtimeDirs := capability.TerminalBrowserHashedDirs(home, getenv)
	for _, d := range dataDirs {
		addWritable(d, false)
	}
	for _, d := range stateDirs {
		addWritable(d, false)
	}
	for _, d := range runtimeDirs {
		addWritable(d, true)
	}

	// 2. Interop directories.
	// terminal-browser's interopRoot() uses $TERMINAL_BROWSER_INTEROP_DIR if
	// absolute, otherwise hardcodes ~/.local/{state,share}/terminal-browser-interop
	// (ignoring XDG_STATE_HOME/XDG_DATA_HOME).
	if override := getenv("TERMINAL_BROWSER_INTEROP_DIR"); override != "" && filepath.IsAbs(override) {
		addWritable(override, true)
	} else {
		addWritable(filepath.Join(home, ".local", "state", "terminal-browser-interop"), true)
		addWritable(filepath.Join(home, ".local", "share", "terminal-browser-interop"), false)
	}

	// 3. Config directory (read-only).
	configDir := filepath.Join(homepath.XDG(getenv("XDG_CONFIG_HOME"), home, ".config"), "terminal-browser")
	if override := getenv("TERMINAL_BROWSER_CONFIG_DIR"); override != "" && filepath.IsAbs(override) {
		configDir = override
	}
	if cfgGranted := checker.GrantDir(configDir, false); len(cfgGranted) > 0 && seatbeltSafe(cfgGranted) {
		addUnique(&out.Readable, cfgGranted...)
	}

	// 4. Scoped macOS Seatbelt Unix-socket outbound rules so terminal-browser
	// can connect to daemon.sock / instances/*.sock / agent-browser/*.sock
	// even under network: none or allow_ports. On Linux Landlock, connecting
	// to filesystem-backed Unix sockets is governed by directory write access
	// (already covered by Writable).
	if goos == "darwin" {
		for _, dir := range socketDirs {
			if pathguard.SafeSeatbeltPath(dir) {
				addUnique(&out.Allow, fmt.Sprintf(`network-outbound (remote unix-socket (subpath "%s"))`, dir))
			}
		}
	}

	return out
}
