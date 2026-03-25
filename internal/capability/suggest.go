package capability

import (
	"os"
	"path/filepath"
	"strings"
)

// SuggestForPath returns capability names that would grant access to the given path.
func SuggestForPath(path string, registry map[string]Capability) []string {
	home, _ := os.UserHomeDir()
	var suggestions []string
	for name, cap := range registry {
		if matchesAnyPath(path, cap.Readable, home) || matchesAnyPath(path, cap.Writable, home) {
			suggestions = append(suggestions, name)
		}
	}
	return suggestions
}

func matchesAnyPath(path string, paths []string, home string) bool {
	for _, p := range paths {
		expanded := expandTilde(p, home)
		if strings.HasPrefix(path, expanded) {
			return true
		}
	}
	return false
}

func expandTilde(path, home string) string {
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:])
	}
	return path
}
