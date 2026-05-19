package fsutil_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/jskswamy/aide/internal/fsutil"
)

func TestResolveOrSelf_NonExistentPath(t *testing.T) {
	// EvalSymlinks errors on ENOENT; the helper must fall back to the
	// input path so first-write-via-symlink callers still receive a
	// usable target.
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	got := fsutil.ResolveOrSelf(missing)
	if got != missing {
		t.Errorf("ResolveOrSelf(%q) = %q, want %q (unchanged on error)", missing, got, missing)
	}
}

func TestResolveOrSelf_RegularPath(t *testing.T) {
	// For an existing non-symlink file the helper must return the input
	// path's canonical form. EvalSymlinks may canonicalize through
	// directory symlinks (e.g. /tmp → /private/tmp on macOS) so we
	// compare against the same canonicalization.
	dir := t.TempDir()
	file := filepath.Join(dir, "regular.txt")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	want, err := filepath.EvalSymlinks(file)
	if err != nil {
		t.Fatalf("canonicalize want: %v", err)
	}
	if got := fsutil.ResolveOrSelf(file); got != want {
		t.Errorf("ResolveOrSelf(%q) = %q, want %q", file, got, want)
	}
}

func TestResolveOrSelf_Symlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks require elevated privileges on windows")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "target.txt")
	link := filepath.Join(dir, "link.txt")
	if err := os.WriteFile(target, []byte("x"), 0o600); err != nil {
		t.Fatalf("seed target: %v", err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	canonTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		t.Fatalf("canonicalize target: %v", err)
	}
	if got := fsutil.ResolveOrSelf(link); got != canonTarget {
		t.Errorf("ResolveOrSelf(%q) = %q, want %q", link, got, canonTarget)
	}
}
