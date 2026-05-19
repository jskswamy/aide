package testutil

import (
	"os"
	"path/filepath"
	"testing"
)

// MakeSymlinkedFile creates a regular file at root/targetRel and a symlink
// at root/declaredRel pointing at it, returning the absolute paths of
// both. Parent directories are created as needed; any failure is reported
// via t.Fatalf so callers do not need to interleave error handling with
// the test setup they care about.
//
// This is the regular-file two-hop case. For directory leaves use
// MakeSymlinkedDir; for chains longer than two hops use MakeSymlinkChain.
func MakeSymlinkedFile(t *testing.T, root, declaredRel, targetRel string) (declared, target string) {
	t.Helper()
	target = filepath.Join(root, targetRel)
	declared = filepath.Join(root, declaredRel)
	if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
		t.Fatalf("MakeSymlinkedFile: mkdir target dir: %v", err)
	}
	if err := os.WriteFile(target, []byte{}, 0o600); err != nil {
		t.Fatalf("MakeSymlinkedFile: write target: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(declared), 0o750); err != nil {
		t.Fatalf("MakeSymlinkedFile: mkdir link parent: %v", err)
	}
	if err := os.Symlink(target, declared); err != nil {
		t.Fatalf("MakeSymlinkedFile: symlink: %v", err)
	}
	return declared, target
}

// MakeSymlinkedDir is the directory-leaf analogue of MakeSymlinkedFile.
// The target is created as a directory (not a regular file) so callers
// can construct scenarios where the agent might try to write inside the
// resolved target — e.g. an aide-secrets directory.
func MakeSymlinkedDir(t *testing.T, root, declaredRel, targetRel string) (declared, target string) {
	t.Helper()
	target = filepath.Join(root, targetRel)
	declared = filepath.Join(root, declaredRel)
	if err := os.MkdirAll(target, 0o750); err != nil {
		t.Fatalf("MakeSymlinkedDir: mkdir target: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(declared), 0o750); err != nil {
		t.Fatalf("MakeSymlinkedDir: mkdir link parent: %v", err)
	}
	if err := os.Symlink(target, declared); err != nil {
		t.Fatalf("MakeSymlinkedDir: symlink: %v", err)
	}
	return declared, target
}

// MakeSymlinkChain creates a chain of symlinks under root from hops[0]
// (the caller-facing "declared" entry point) through every intermediate
// hop to hops[len-1] (a regular-file leaf). Each hops[i] is a path
// relative to root. Returns the absolute paths in the same order. At
// least two hops are required; otherwise the call is meaningless and
// t.Fatalf is invoked.
//
// Use this when you need to simulate nix-home-manager-style chains
// (declared symlink → nix-store hop → user-edited file under $HOME).
// The leaf is always a regular file; if you need a directory leaf,
// open a follow-up — no current call site needs that combination.
func MakeSymlinkChain(t *testing.T, root string, hops []string) []string {
	t.Helper()
	if len(hops) < 2 {
		t.Fatalf("MakeSymlinkChain: need at least 2 hops, got %d", len(hops))
	}
	abs := make([]string, len(hops))
	for i, h := range hops {
		abs[i] = filepath.Join(root, h)
	}
	leaf := abs[len(abs)-1]
	if err := os.MkdirAll(filepath.Dir(leaf), 0o750); err != nil {
		t.Fatalf("MakeSymlinkChain: mkdir leaf dir: %v", err)
	}
	if err := os.WriteFile(leaf, []byte{}, 0o600); err != nil {
		t.Fatalf("MakeSymlinkChain: write leaf: %v", err)
	}
	for i := len(abs) - 2; i >= 0; i-- {
		if err := os.MkdirAll(filepath.Dir(abs[i]), 0o750); err != nil {
			t.Fatalf("MakeSymlinkChain: mkdir hop %d parent: %v", i, err)
		}
		if err := os.Symlink(abs[i+1], abs[i]); err != nil {
			t.Fatalf("MakeSymlinkChain: symlink hop %d -> %d: %v", i, i+1, err)
		}
	}
	return abs
}
