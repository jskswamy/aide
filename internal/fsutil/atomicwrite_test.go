package fsutil_test

import (
	"os"
	"path/filepath"
	"runtime"
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
