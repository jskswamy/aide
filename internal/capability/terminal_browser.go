package capability

import (
	"os"
	"path/filepath"
	"regexp"

	"github.com/jskswamy/aide/internal/homepath"
)

var terminalBrowserDirRe = regexp.MustCompile(`^terminal-browser-(dev-)?[0-9a-f]{8}$`)

// TerminalBrowserHashedDirs scans the XDG data, state, and runtime roots for
// existing terminal-browser-<8-hex> and terminal-browser-dev-<8-hex>
// directories. Symlinked directories are included (via os.Stat), while regular
// files and broken symlinks are ignored.
func TerminalBrowserHashedDirs(homeDir string, getenv func(string) string) (dataDirs, stateDirs, runtimeDirs []string) {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	dataHome := homepath.XDG(getenv("XDG_DATA_HOME"), homeDir, ".local/share")
	stateHome := homepath.XDG(getenv("XDG_STATE_HOME"), homeDir, ".local/state")
	runtimeHome := stateHome
	if rt := getenv("XDG_RUNTIME_DIR"); rt != "" && filepath.IsAbs(rt) {
		runtimeHome = filepath.Clean(rt)
	}

	dataDirs = scanTerminalBrowserDirs(dataHome)
	stateDirs = scanTerminalBrowserDirs(stateHome)
	if runtimeHome == stateHome {
		runtimeDirs = stateDirs
	} else {
		runtimeDirs = scanTerminalBrowserDirs(runtimeHome)
	}
	return dataDirs, stateDirs, runtimeDirs
}

func scanTerminalBrowserDirs(root string) []string {
	if root == "" {
		return nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var dirs []string
	for _, entry := range entries {
		if !terminalBrowserDirRe.MatchString(entry.Name()) {
			continue
		}
		full := filepath.Join(root, entry.Name())
		if info, err := os.Stat(full); err == nil && info.IsDir() {
			dirs = append(dirs, full)
		}
	}
	return dirs
}
