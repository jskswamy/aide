package sandbox

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDeriveGrantedPathSet_ProjectRootIsWritable verifies the project root is writable.
func TestDeriveGrantedPathSet_ProjectRootIsWritable(t *testing.T) {
	dir := t.TempDir()
	policy := Policy{
		ProjectRoot: dir,
		Guards:      []string{},
	}
	gps := DeriveGrantedPathSet(policy)

	if !containsPath(gps.Writable, dir) {
		t.Errorf("ProjectRoot %q not in Writable: %v", dir, gps.Writable)
	}
}

// TestDeriveGrantedPathSet_DenyWins ensures a denied path is excluded from Writable.
func TestDeriveGrantedPathSet_DenyWins(t *testing.T) {
	dir := t.TempDir()
	policy := Policy{
		ProjectRoot: dir,
		ExtraDenied: []string{dir},
		Guards:      []string{},
	}
	gps := DeriveGrantedPathSet(policy)

	if containsPath(gps.Writable, dir) {
		t.Errorf("denied path %q must not appear in Writable", dir)
	}
	if !containsPath(gps.Denied, dir) {
		t.Errorf("denied path %q must appear in Denied: %v", dir, gps.Denied)
	}
}

// TestDeriveGrantedPathSet_ExtraDeniedAppearsInDenied verifies explicit deny paths.
func TestDeriveGrantedPathSet_ExtraDeniedAppearsInDenied(t *testing.T) {
	secret := t.TempDir()
	policy := Policy{
		ExtraDenied: []string{secret},
		Guards:      []string{},
	}
	gps := DeriveGrantedPathSet(policy)

	if !containsPath(gps.Denied, secret) {
		t.Errorf("ExtraDenied path %q not in Denied: %v", secret, gps.Denied)
	}
}

// TestDeriveGrantedPathSet_OriginGuardPopulated verifies origin tracking.
func TestDeriveGrantedPathSet_OriginGuardPopulated(t *testing.T) {
	dir := t.TempDir()
	policy := Policy{
		ProjectRoot: dir,
		Guards:      []string{},
	}
	gps := DeriveGrantedPathSet(policy)

	if gps.OriginGuard == nil {
		t.Fatal("OriginGuard must not be nil")
	}
	resolved, _ := filepath.EvalSymlinks(dir)
	if gps.OriginGuard[resolved] == "" {
		t.Errorf("OriginGuard missing entry for ProjectRoot %q (resolved: %q)", dir, resolved)
	}
}

// TestDeriveGrantedPathSet_ExtraWritableIsWritable checks extra_writable config paths.
func TestDeriveGrantedPathSet_ExtraWritableIsWritable(t *testing.T) {
	extra := t.TempDir()
	policy := Policy{
		ExtraWritable: []string{extra},
		Guards:        []string{},
	}
	gps := DeriveGrantedPathSet(policy)

	if !containsPath(gps.Writable, extra) {
		t.Errorf("ExtraWritable path %q not in Writable: %v", extra, gps.Writable)
	}
}

// TestDeriveGrantedPathSet_ExtraReadableIsReadable checks extra_readable config paths.
func TestDeriveGrantedPathSet_ExtraReadableIsReadable(t *testing.T) {
	extra := t.TempDir()
	policy := Policy{
		ExtraReadable: []string{extra},
		Guards:        []string{},
	}
	gps := DeriveGrantedPathSet(policy)

	if !containsPath(gps.Readable, extra) {
		t.Errorf("ExtraReadable path %q not in Readable: %v", extra, gps.Readable)
	}
}

// TestDeriveGrantedPathSet_SymlinkResolution verifies symlinks are resolved.
func TestDeriveGrantedPathSet_SymlinkResolution(t *testing.T) {
	realDir := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(realDir, link); err != nil {
		t.Skip("cannot create symlink:", err)
	}
	policy := Policy{
		ProjectRoot: link,
		Guards:      []string{},
	}
	gps := DeriveGrantedPathSet(policy)

	// Writable must contain the resolved real path, not the symlink.
	realResolved, _ := filepath.EvalSymlinks(realDir)
	if !containsPath(gps.Writable, realResolved) {
		t.Errorf("Writable should contain resolved real path %q, got: %v", realResolved, gps.Writable)
	}
}

// TestDeriveGrantedPathSet_HomeSSHNotInReadable verifies ~/.ssh is not broadly readable.
func TestDeriveGrantedPathSet_HomeSSHNotInReadable(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home dir")
	}
	sshDir := filepath.Join(home, ".ssh")

	policy := Policy{
		Guards: []string{}, // no guards = no guard-derived allows
	}
	gps := DeriveGrantedPathSet(policy)

	if containsPath(gps.Readable, sshDir) || containsPath(gps.Writable, sshDir) {
		t.Errorf("~/.ssh must not appear in Readable or Writable with default guards (no coarse $HOME allow)")
	}
}

// containsPath checks if a path or its EvalSymlinks-resolved form appears in list.
func containsPath(list []string, target string) bool {
	resolved, _ := filepath.EvalSymlinks(target)
	for _, p := range list {
		if p == target || p == resolved {
			return true
		}
	}
	return false
}
