package trust

import (
	"github.com/jskswamy/aide/internal/testutil"
	"os"
	"path/filepath"
	"testing"
)

func TestCanonicalPathResolvesSymlinks(t *testing.T) {
	dir := t.TempDir()
	realDir := filepath.Join(dir, "real")
	if err := os.Mkdir(realDir, 0o750); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(realDir, ".aide.yaml")
	if err := os.WriteFile(file, []byte("agent: claude\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(realDir, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	want, err := filepath.EvalSymlinks(file)
	if err != nil {
		t.Fatal(err)
	}
	got, err := CanonicalPath(filepath.Join(link, ".aide.yaml"))
	if err != nil {
		t.Fatalf("CanonicalPath: %v", err)
	}
	if got != want {
		t.Errorf("CanonicalPath = %q, want %q", got, want)
	}
}

func TestCanonicalPathMissingFileFallsBackToAbsolute(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "nope", ".aide.yaml")
	got, err := CanonicalPath(missing)
	if err == nil {
		t.Fatal("want an error for a missing file")
	}
	if got != missing {
		t.Errorf("CanonicalPath = %q, want the absolute path %q", got, missing)
	}
}

// Trust given through a symlinked directory is the same approval as
// trust given through the real path: both key on the resolved path.
func TestCheckFileSharesApprovalAcrossSymlinks(t *testing.T) {
	dir := t.TempDir()
	realDir := filepath.Join(dir, "real")
	if err := os.Mkdir(realDir, 0o750); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(realDir, ".aide.yaml")
	if err := os.WriteFile(file, []byte("agent: claude\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(realDir, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	store := NewStore(t.TempDir())

	fc, err := store.CheckFile(filepath.Join(link, ".aide.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if fc.Status != Untrusted {
		t.Fatalf("status = %v, want untrusted", fc.Status)
	}
	if err := store.Trust(fc.Path, fc.Contents); err != nil {
		t.Fatal(err)
	}
	fc, err = store.CheckFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if fc.Status != Trusted {
		t.Errorf("status via real path = %v, want trusted", fc.Status)
	}

	if err := os.WriteFile(file, []byte("agent: codex\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fc, err = store.CheckFile(filepath.Join(link, ".aide.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if fc.Status != Untrusted {
		t.Errorf("status after change = %v, want untrusted", fc.Status)
	}
}

func TestCheckFileMissing(t *testing.T) {
	store := NewStore(t.TempDir())
	if _, err := store.CheckFile(filepath.Join(t.TempDir(), ".aide.yaml")); err == nil {
		t.Error("want an error for a missing file")
	}
}

func TestCheckContentsJudgesGivenBytes(t *testing.T) {
	dir := testutil.CanonicalTempDir(t)
	file := filepath.Join(dir, ".aide.yaml")
	if err := os.WriteFile(file, []byte("agent: claude\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewStore(t.TempDir())
	if err := store.Trust(file, []byte("agent: claude\n")); err != nil {
		t.Fatal(err)
	}
	if got := store.CheckContents(file, []byte("agent: claude\n")).Status; got != Trusted {
		t.Errorf("same bytes: %v, want trusted", got)
	}
	if got := store.CheckContents(file, []byte("agent: codex\n")).Status; got != Untrusted {
		t.Errorf("other bytes: %v, want untrusted", got)
	}
}
