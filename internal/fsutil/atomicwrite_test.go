package fsutil_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/fsutil"
)

func TestAtomicWriteCreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")

	if err := fsutil.AtomicWrite(path, []byte("hello")); err != nil {
		t.Fatalf("AtomicWrite: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "hello" {
		t.Errorf("contents = %q, want %q", got, "hello")
	}
}

func TestAtomicWriteOverwrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")

	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := fsutil.AtomicWrite(path, []byte("new")); err != nil {
		t.Fatalf("AtomicWrite: %v", err)
	}

	got, _ := os.ReadFile(path)
	if string(got) != "new" {
		t.Errorf("contents = %q, want %q", got, "new")
	}
}

func TestAtomicWriteCreatesParentDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "nested", "out.txt")

	if err := fsutil.AtomicWrite(path, []byte("x")); err != nil {
		t.Fatalf("AtomicWrite: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("stat: %v", err)
	}
	parent := filepath.Dir(path)
	info, err := os.Stat(parent)
	if err != nil {
		t.Fatalf("stat parent: %v", err)
	}
	if runtime.GOOS != "windows" {
		if perm := info.Mode().Perm(); perm != 0o750 {
			t.Errorf("parent perm = %o, want 0o750", perm)
		}
	}
}

func TestAtomicWriteFileMode0600(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file mode bits not enforced on windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")

	if err := fsutil.AtomicWrite(path, []byte("x")); err != nil {
		t.Fatalf("AtomicWrite: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("file perm = %o, want 0o600", perm)
	}
}

func TestAtomicWriteOverwritePreservesModeAfterChmod(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file mode bits not enforced on windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")

	if err := fsutil.AtomicWrite(path, []byte("a")); err != nil {
		t.Fatalf("AtomicWrite: %v", err)
	}
	// Even if the existing file had wider perms, an overwrite must land at 0o600.
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	if err := fsutil.AtomicWrite(path, []byte("b")); err != nil {
		t.Fatalf("AtomicWrite (overwrite): %v", err)
	}
	info, _ := os.Stat(path)
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("after overwrite perm = %o, want 0o600", perm)
	}
}

func TestAtomicWriteParentIsFileFailsMkdirAll(t *testing.T) {
	// AtomicWrite calls MkdirAll on filepath.Dir(path). When that ancestor
	// path is already a regular file, MkdirAll returns "not a directory"
	// and AtomicWrite must surface a wrapped error and write nothing.
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("file, not a dir"), 0o600); err != nil {
		t.Fatalf("seed blocker: %v", err)
	}
	path := filepath.Join(blocker, "nested", "out.txt")

	err := fsutil.AtomicWrite(path, []byte("x"))
	if err == nil {
		t.Fatal("expected error when parent path is a regular file")
	}
	if !strings.Contains(err.Error(), "creating parent directory") {
		t.Errorf("error %q should mention parent directory", err)
	}
}

func TestAtomicWriteReadOnlyDirFailsCreateTemp(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix-only: chmod 0o500 doesn't deny writes on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory write bit")
	}
	dir := t.TempDir()
	subdir := filepath.Join(dir, "ro")
	if err := os.Mkdir(subdir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Strip write bit so CreateTemp inside subdir fails with EACCES.
	if err := os.Chmod(subdir, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(subdir, 0o700) })

	err := fsutil.AtomicWrite(filepath.Join(subdir, "out.txt"), []byte("x"))
	if err == nil {
		t.Fatal("expected error when parent dir is not writable")
	}
	if !strings.Contains(err.Error(), "creating temp file") {
		t.Errorf("error %q should mention temp file creation", err)
	}
	// Sanity: no temp leftovers (write bit was off, so this is structural).
	entries, _ := os.ReadDir(subdir)
	if len(entries) != 0 {
		t.Errorf("read-only dir should be empty, found %d entries", len(entries))
	}
}

func TestAtomicWriteRenameOntoNonEmptyDirFails(t *testing.T) {
	// On POSIX, renaming a regular file onto a non-empty directory fails
	// with ENOTEMPTY/EISDIR. Use this to exercise the rename-error branch
	// and verify the temp file is cleaned up.
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	// Make target a non-empty directory.
	if err := os.MkdirAll(filepath.Join(target, "child"), 0o700); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}

	err := fsutil.AtomicWrite(target, []byte("x"))
	if err == nil {
		t.Fatal("expected error when renaming over a non-empty directory")
	}
	if !strings.Contains(err.Error(), "renaming temp file") {
		t.Errorf("error %q should mention rename", err)
	}
	// Cleanup contract: the temp file in dir must be gone after rename
	// failure. Anything left other than `target/` is a leak.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, e := range entries {
		if e.Name() == "target" {
			continue
		}
		t.Errorf("rename failure left temp file %q in dir", e.Name())
	}
}

func TestAtomicWriteLeavesNoTempFilesOnSuccess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")

	if err := fsutil.AtomicWrite(path, []byte("data")); err != nil {
		t.Fatalf("AtomicWrite: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, e := range entries {
		if e.Name() == "out.txt" {
			continue
		}
		t.Errorf("unexpected leftover entry %q", e.Name())
	}
}
