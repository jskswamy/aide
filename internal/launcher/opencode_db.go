package launcher

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

// opensOpenCodeProfileDB reports whether an OpenCode launch with these
// final args opens its own database: it starts a private server, or it
// is debug paths, which prints this process's database path. Commands
// that start or manage the shared background server do not, or a
// profile's accounts would be served to every other OpenCode client.
func opensOpenCodeProfileDB(args []string) bool {
	if len(args) >= 2 && args[0] == "debug" && args[1] == "paths" {
		return true
	}
	return slices.Contains(args, "--standalone")
}

// openCodeProfileDB returns <OPENCODE_CONFIG_DIR>/opencode.db from env,
// the OPENCODE_DB value for a launch whose profile selects a config
// dir. OpenCode resolves a relative OPENCODE_DB under the shared data
// dir, so a path that is not absolute is an error, as is a
// never_allow_env entry that would strip it. The file is not created.
func openCodeProfileDB(env, blocked []string) (string, error) {
	if slices.Contains(blocked, "OPENCODE_DB") {
		return "", errors.New("opencode profile database path is blocked by never_allow_env")
	}
	var path string
	for _, e := range env {
		if dir, ok := strings.CutPrefix(e, "OPENCODE_CONFIG_DIR="); ok && dir != "" {
			path = filepath.Join(dir, "opencode.db")
		}
	}
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("opencode profile database path is not absolute: %s", path)
	}
	return path, nil
}
