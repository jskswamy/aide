// Package testutil holds helpers shared across the repo's test suites.
//
// All symbols here assume *testing.T and should only be referenced from
// _test.go files. They are not part of any runtime API.
package testutil

import (
	"path/filepath"
	"testing"
)

// CanonicalTempDir returns a per-test scratch directory whose path has
// been resolved through any directory symlinks. On macOS the standard
// t.TempDir returns paths under /var/folders/... — itself a symlink to
// /private/var/folders/... — and tests that compare against
// kernel-resolved paths (the form seatbelt subpath rules match, the
// form filepath.EvalSymlinks returns, the form realpath(3) reports)
// will silently see mismatches without this canonicalization.
//
// The dir is registered with t.TempDir's cleanup, so callers do not
// need to remove it manually.
func CanonicalTempDir(t *testing.T) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("CanonicalTempDir: EvalSymlinks: %v", err)
	}
	return resolved
}
