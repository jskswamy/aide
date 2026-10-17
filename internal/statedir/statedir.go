// Package statedir locates aide's user-local state files (provisioning
// ownership, repository preferences). They live in one directory:
// $XDG_STATE_HOME/aide when XDG_STATE_HOME is an absolute path, else
// ~/.local/state/aide.
package statedir

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/jskswamy/aide/internal/homepath"
)

const appName = "aide"

// Path returns the state file name under aide's state directory for
// homeDir. When XDG_STATE_HOME moves the directory away from
// ~/.local/state but the file still exists only at the old location,
// the old path is returned, so setting the variable never orphans
// existing state; the file moves when the user moves it.
func Path(homeDir, name string) string {
	legacy := filepath.Join(homeDir, ".local", "state", appName, name)
	current := filepath.Join(homepath.XDG(os.Getenv("XDG_STATE_HOME"), homeDir, ".local/state"), appName, name)
	if current == legacy || exists(current) || !exists(legacy) {
		return current
	}
	return legacy
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return !errors.Is(err, os.ErrNotExist)
}
