package trust

import (
	"fmt"
	"os"
	"path/filepath"
)

// CanonicalPath returns the identity trust and deny records key on:
// the file's absolute path with its parent directories
// symlink-resolved, so a checkout reached through a symlinked
// directory has one approval. The final component is not followed: a
// symlinked .aide.yaml is its own file, and approving its target
// elsewhere never approves the link. When the parent cannot be
// resolved (for example it does not exist) it returns the absolute
// path and the error.
func CanonicalPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path, err
	}
	dir, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil {
		return abs, err
	}
	return filepath.Join(dir, filepath.Base(abs)), nil
}

// symlinkTarget is where path points when it is a symlink, else "".
func symlinkTarget(path string) string {
	fi, err := os.Lstat(path)
	if err != nil || fi.Mode()&os.ModeSymlink == 0 {
		return ""
	}
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		target, _ = os.Readlink(path)
	}
	return target
}

// FileCheck is the trust decision for one file: its canonical path,
// the exact contents that were judged, and the status.
type FileCheck struct {
	Path     string
	Contents []byte
	Status   Status
	// Target is where Path points when it is a symlink; display only.
	Target string
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
	return FileCheck{Path: canonical, Contents: contents, Status: s.Check(canonical, contents), Target: symlinkTarget(canonical)}
}
