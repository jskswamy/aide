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

// TestCheckSymlinkCycle pins the "loud-fail on cycle, silent-pass on
// everything else" contract that capability config-load relies on.
// Cycles must produce a non-nil error so the surrounding wrapper can
// surface the offending path; missing paths and non-symlinks must NOT
// (a capability declaring a not-yet-created cache dir is normal).
func TestCheckSymlinkCycle(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks require elevated privileges on windows")
	}
	dir := t.TempDir()

	t.Run("cycle returns error", func(t *testing.T) {
		a := filepath.Join(dir, "cycle-a")
		b := filepath.Join(dir, "cycle-b")
		if err := os.Symlink(b, a); err != nil {
			t.Fatalf("symlink a: %v", err)
		}
		if err := os.Symlink(a, b); err != nil {
			t.Fatalf("symlink b: %v", err)
		}
		if err := fsutil.CheckSymlinkCycle(a); err == nil {
			t.Errorf("CheckSymlinkCycle(%q): expected error, got nil", a)
		}
	})

	t.Run("missing path is fine", func(t *testing.T) {
		missing := filepath.Join(dir, "does-not-exist")
		if err := fsutil.CheckSymlinkCycle(missing); err != nil {
			t.Errorf("CheckSymlinkCycle(%q) for ENOENT path: want nil, got %v", missing, err)
		}
	})

	t.Run("regular file is fine", func(t *testing.T) {
		regular := filepath.Join(dir, "regular.txt")
		if err := os.WriteFile(regular, []byte("x"), 0o600); err != nil {
			t.Fatalf("seed: %v", err)
		}
		if err := fsutil.CheckSymlinkCycle(regular); err != nil {
			t.Errorf("CheckSymlinkCycle(%q) for plain file: want nil, got %v", regular, err)
		}
	})

	t.Run("non-cycle symlink chain is fine", func(t *testing.T) {
		target := filepath.Join(dir, "chain-target.txt")
		hop := filepath.Join(dir, "chain-hop")
		link := filepath.Join(dir, "chain-link")
		if err := os.WriteFile(target, []byte("x"), 0o600); err != nil {
			t.Fatalf("seed target: %v", err)
		}
		if err := os.Symlink(target, hop); err != nil {
			t.Fatalf("symlink hop: %v", err)
		}
		if err := os.Symlink(hop, link); err != nil {
			t.Fatalf("symlink link: %v", err)
		}
		if err := fsutil.CheckSymlinkCycle(link); err != nil {
			t.Errorf("CheckSymlinkCycle(%q) for two-hop chain: want nil, got %v", link, err)
		}
	})
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
