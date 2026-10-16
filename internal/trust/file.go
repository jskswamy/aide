package trust

import (
	"fmt"
	"os"
	"path/filepath"
)

// CanonicalPath returns the absolute, symlink-resolved path that trust
// and deny records key on, so one file reached through different
// symlinks has one approval. When the path cannot be resolved (for
// example it does not exist) it returns the absolute path and the
// error.
func CanonicalPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path, err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return abs, err
	}
	return resolved, nil
}

// FileCheck is the trust decision for one file: its canonical path,
// the exact contents that were judged, and the status.
type FileCheck struct {
	Path     string
	Contents []byte
	Status   Status
}

// CheckFile resolves path with CanonicalPath, reads it, and returns its
// trust status. It is the single trust check every caller shares.
func (s *Store) CheckFile(path string) (FileCheck, error) {
	canonical, err := CanonicalPath(path)
	if err != nil {
		return FileCheck{Path: canonical}, fmt.Errorf("resolving %s: %w", path, err)
	}
	contents, err := os.ReadFile(canonical) // #nosec G304 -- the user's own .aide.yaml
	if err != nil {
		return FileCheck{Path: canonical}, err
	}
	return s.CheckContents(canonical, contents), nil
}

// CheckContents judges contents already read from path, keyed on the
// canonical path. Callers that parsed a file use it so the bytes
// judged are the bytes applied.
func (s *Store) CheckContents(path string, contents []byte) FileCheck {
	canonical, _ := CanonicalPath(path)
	return FileCheck{Path: canonical, Contents: contents, Status: s.Check(canonical, contents)}
}
