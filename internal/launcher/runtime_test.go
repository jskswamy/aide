package launcher

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestNewRuntimeDir_Creates(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", base)

	rd, err := NewRuntimeDir()
	if err != nil {
		t.Fatalf("NewRuntimeDir() error: %v", err)
	}
	defer rd.Cleanup() //nolint:errcheck

	info, err := os.Stat(rd.Path())
	if err != nil {
		t.Fatalf("stat runtime dir: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("runtime dir is not a directory")
	}
	perm := info.Mode().Perm()
	if perm != 0700 {
		t.Errorf("expected mode 0700, got %04o", perm)
	}
}

func TestNewRuntimeDir_UsesXDGRuntimeDir(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", base)

	rd, err := NewRuntimeDir()
	if err != nil {
		t.Fatalf("NewRuntimeDir() error: %v", err)
	}
	defer rd.Cleanup() //nolint:errcheck

	expected := filepath.Join(base, fmt.Sprintf("aide-%d", os.Getpid()))
	if rd.Path() != expected {
		t.Errorf("expected path %s, got %s", expected, rd.Path())
	}
}

func TestNewRuntimeDir_FallsBackToTempDir(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "")

	rd, err := NewRuntimeDir()
	if err != nil {
		t.Fatalf("NewRuntimeDir() error: %v", err)
	}
	defer rd.Cleanup() //nolint:errcheck

	// Should be under os.TempDir()
	tmpDir := os.TempDir()
	if !filepath.HasPrefix(rd.Path(), tmpDir) {
		t.Errorf("expected path under %s, got %s", tmpDir, rd.Path())
	}
}

func TestRuntimeDir_PathContainsPID(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", base)

	rd, err := NewRuntimeDir()
	if err != nil {
		t.Fatalf("NewRuntimeDir() error: %v", err)
	}
	defer rd.Cleanup() //nolint:errcheck

	pidStr := fmt.Sprintf("aide-%d", os.Getpid())
	if !contains(rd.Path(), pidStr) {
		t.Errorf("path %s does not contain PID segment %s", rd.Path(), pidStr)
	}
}

func TestRuntimeDir_Cleanup(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", base)

	rd, err := NewRuntimeDir()
	if err != nil {
		t.Fatalf("NewRuntimeDir() error: %v", err)
	}

	// Write a file into the runtime dir
	testFile := filepath.Join(rd.Path(), "test.txt")
	if err := os.WriteFile(testFile, []byte("hello"), 0600); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	if err := rd.Cleanup(); err != nil {
		t.Fatalf("Cleanup() error: %v", err)
	}

	if _, err := os.Stat(rd.Path()); !os.IsNotExist(err) {
		t.Errorf("expected directory to be removed, but it still exists")
	}
}

func TestRuntimeDir_CleanupIdempotent(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", base)

	rd, err := NewRuntimeDir()
	if err != nil {
		t.Fatalf("NewRuntimeDir() error: %v", err)
	}

	if err := rd.Cleanup(); err != nil {
		t.Fatalf("first Cleanup() error: %v", err)
	}

	// Second call should not error
	if err := rd.Cleanup(); err != nil {
		t.Fatalf("second Cleanup() error: %v", err)
	}
}

func TestCleanStale_RemovesOrphanedDirs(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", base)

	// Create a directory for a PID that almost certainly doesn't exist
	staleDir := filepath.Join(base, "aide-99999999")
	if err := os.MkdirAll(staleDir, 0700); err != nil {
		t.Fatalf("create stale dir: %v", err)
	}

	if err := CleanStale(); err != nil {
		t.Fatalf("CleanStale() error: %v", err)
	}

	if _, err := os.Stat(staleDir); !os.IsNotExist(err) {
		t.Errorf("expected stale dir to be removed, but it still exists")
	}
}

func TestCleanStale_PreservesLiveDirs(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", base)

	// Create a directory for the current process (which is alive)
	liveDir := filepath.Join(base, fmt.Sprintf("aide-%d", os.Getpid()))
	if err := os.MkdirAll(liveDir, 0700); err != nil {
		t.Fatalf("create live dir: %v", err)
	}

	if err := CleanStale(); err != nil {
		t.Fatalf("CleanStale() error: %v", err)
	}

	if _, err := os.Stat(liveDir); os.IsNotExist(err) {
		t.Errorf("expected live dir to be preserved, but it was removed")
	}
}

func TestNewRuntimeDir_ReplacesExisting(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", base)

	// Pre-create the directory with a file
	existingDir := filepath.Join(base, fmt.Sprintf("aide-%d", os.Getpid()))
	if err := os.MkdirAll(existingDir, 0700); err != nil {
		t.Fatalf("create existing dir: %v", err)
	}
	oldFile := filepath.Join(existingDir, "old.txt")
	if err := os.WriteFile(oldFile, []byte("stale"), 0600); err != nil {
		t.Fatalf("write old file: %v", err)
	}

	rd, err := NewRuntimeDir()
	if err != nil {
		t.Fatalf("NewRuntimeDir() error: %v", err)
	}
	defer rd.Cleanup() //nolint:errcheck

	// Old file should be gone
	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Errorf("expected old file to be removed after NewRuntimeDir replaced existing dir")
	}

	// New dir should exist
	info, err := os.Stat(rd.Path())
	if err != nil {
		t.Fatalf("stat new dir: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("new dir is not a directory")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
