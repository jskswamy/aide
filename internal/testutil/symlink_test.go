package testutil_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jskswamy/aide/internal/testutil"
)

// TestMakeSymlinkedFile pins the leaf-is-regular-file contract. After the
// call, declared must be a symlink and target must be a regular file with
// the absolute paths the caller can use directly in seatbelt rules.
func TestMakeSymlinkedFile(t *testing.T) {
	root := testutil.CanonicalTempDir(t)
	declared, target := testutil.MakeSymlinkedFile(t, root, "link/foo.yaml", "real/foo.yaml")

	if declared != filepath.Join(root, "link/foo.yaml") {
		t.Errorf("declared = %q, want path under root", declared)
	}
	if target != filepath.Join(root, "real/foo.yaml") {
		t.Errorf("target = %q, want path under root", target)
	}
	if info, err := os.Lstat(declared); err != nil {
		t.Fatalf("lstat declared: %v", err)
	} else if info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("declared is not a symlink: %v", info.Mode())
	}
	if info, err := os.Stat(target); err != nil {
		t.Fatalf("stat target: %v", err)
	} else if !info.Mode().IsRegular() {
		t.Errorf("target is not a regular file: %v", info.Mode())
	}
	if got, err := filepath.EvalSymlinks(declared); err != nil {
		t.Fatalf("eval: %v", err)
	} else if got != target {
		t.Errorf("EvalSymlinks(%q) = %q, want %q", declared, got, target)
	}
}

// TestMakeSymlinkedDir is the dir-leaf analogue — needed by aide-secrets
// tests where the leaf is a directory the agent might try to write into.
func TestMakeSymlinkedDir(t *testing.T) {
	root := testutil.CanonicalTempDir(t)
	declared, target := testutil.MakeSymlinkedDir(t, root, "link-dir", "real-dir")

	if info, err := os.Stat(target); err != nil {
		t.Fatalf("stat target: %v", err)
	} else if !info.IsDir() {
		t.Errorf("target is not a directory: %v", info.Mode())
	}
	if got, err := filepath.EvalSymlinks(declared); err != nil {
		t.Fatalf("eval: %v", err)
	} else if got != target {
		t.Errorf("EvalSymlinks(%q) = %q, want %q", declared, got, target)
	}
}

// TestMakeSymlinkChain pins the N-hop contract. The first entry is the
// caller-facing "declared" path; the last entry is the regular-file leaf;
// every intermediate entry is a symlink to the next. EvalSymlinks on the
// declared path must traverse the whole chain to the final target.
func TestMakeSymlinkChain(t *testing.T) {
	root := testutil.CanonicalTempDir(t)
	paths := testutil.MakeSymlinkChain(t, root, []string{
		"home/.config/bd/config.yaml",
		"nix-store-fake/config.yaml",
		"home/nixos-config/config.yaml",
	})

	if len(paths) != 3 {
		t.Fatalf("len(paths) = %d, want 3", len(paths))
	}
	if got, err := filepath.EvalSymlinks(paths[0]); err != nil {
		t.Fatalf("eval declared: %v", err)
	} else if got != paths[2] {
		t.Errorf("EvalSymlinks(%q) = %q, want final target %q", paths[0], got, paths[2])
	}
	if info, err := os.Stat(paths[2]); err != nil {
		t.Fatalf("stat target: %v", err)
	} else if !info.Mode().IsRegular() {
		t.Errorf("final hop %q is not a regular file: %v", paths[2], info.Mode())
	}
	if info, err := os.Lstat(paths[1]); err != nil {
		t.Fatalf("lstat intermediate: %v", err)
	} else if info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("intermediate hop %q is not a symlink: %v", paths[1], info.Mode())
	}
}
