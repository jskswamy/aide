package sandbox

import (
	"os"
	"path/filepath"
	"strings"
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

// TestDeriveGrantedPathSet_AideSecretsNotSubtreeCovered locks down the fix
// for a real vulnerability: an earlier version added ~/.config/aide,
// ~/.local/share/aide and ~/.cache/aide unconditionally to the readable set.
// Landlock has no deny rules and the deny-wins removal in DeriveGrantedPathSet
// is exact-match only, so the AideSecretsGuard's Protected entry for
// ~/.config/aide/secrets did not actually withdraw access — the parent
// subtree allow swallowed the deny and the SOPS-encrypted secret blobs
// became readable + exfiltratable by any sandboxed agent with outbound net.
//
// Invariants this test enforces:
//   - None of ~/.config/aide, ~/.local/share/aide, ~/.cache/aide appears as
//     an exact Readable / Writable entry from "guard:filesystem".
//   - The secrets dir is not subtree-covered by any Readable entry.
func TestDeriveGrantedPathSet_AideSecretsNotSubtreeCovered(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home dir")
	}
	secretsDir := filepath.Join(home, ".config", "aide", "secrets")
	if err := os.MkdirAll(secretsDir, 0o700); err != nil {
		t.Skipf("cannot create secrets dir: %v", err)
	}
	resolvedSecrets, _ := filepath.EvalSymlinks(secretsDir)

	policy := Policy{
		Guards:      []string{"aide-secrets"},
		ProjectRoot: t.TempDir(),
		TempDir:     "/tmp",
	}
	gps := DeriveGrantedPathSet(policy)

	bannedExact := []string{
		filepath.Join(home, ".config", "aide"),
		filepath.Join(home, ".local", "share", "aide"),
		filepath.Join(home, ".cache", "aide"),
	}
	for _, banned := range bannedExact {
		resolved, _ := filepath.EvalSymlinks(banned)
		for _, p := range gps.Readable {
			if p == banned || (resolved != "" && p == resolved) {
				t.Errorf("aide state dir %q must not be unconditionally readable (Origin=%q); use a per-agent grant instead", banned, gps.OriginGuard[p])
			}
		}
		for _, p := range gps.Writable {
			if p == banned || (resolved != "" && p == resolved) {
				t.Errorf("aide state dir %q must not be unconditionally writable (Origin=%q)", banned, gps.OriginGuard[p])
			}
		}
	}

	for _, p := range gps.Readable {
		if p == resolvedSecrets || strings.HasPrefix(resolvedSecrets, p+string(filepath.Separator)) {
			t.Errorf("secrets dir %q is subtree-covered by Readable allow %q; SOPS-encrypted blobs would leak to the agent", resolvedSecrets, p)
		}
	}
	for _, p := range gps.Writable {
		if p == resolvedSecrets || strings.HasPrefix(resolvedSecrets, p+string(filepath.Separator)) {
			t.Errorf("secrets dir %q is subtree-covered by Writable allow %q", resolvedSecrets, p)
		}
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
