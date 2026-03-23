// pkg/seatbelt/path_helpers.go
package seatbelt

import (
	"os"
	"path/filepath"
	"strings"
)

// ExistsOrUnderHome returns true if path exists on disk, or if it's
// under homeDir. Agents create config dirs on first run, so paths
// under home must be writable even before they exist.
//
// Note: this is stricter than the previous defaultDirs helper which used
// strings.HasPrefix(p, homeDir) — that would incorrectly match
// /Users/subramkfoo when homeDir is /Users/subramk. The trailing
// separator check is an intentional correctness fix.
func ExistsOrUnderHome(homeDir, path string) bool {
	if _, err := os.Lstat(path); err == nil {
		return true
	}
	return strings.HasPrefix(path, homeDir+string(filepath.Separator))
}
