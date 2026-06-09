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

// TestDeriveGrantedPathSet_GuardReadablePathsReachReadable verifies that a
// guard's first-class read-only grants (GuardResult.Readable) flow into
// GrantedPathSet.Readable for Landlock enforcement. This is the regression pin
// for the git-config gap: the git-integration guard expressed its grants only
// as Seatbelt file-read* Rules (which the Linux backend does not parse), so
// ~/.gitconfig was denied under Landlock even though "git is enabled".
func TestDeriveGrantedPathSet_GuardReadablePathsReachReadable(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".gitconfig"), []byte("[user]\n\tname = Test\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	policy := Policy{
		HomeDir: home,
		Guards:  []string{"git-integration"},
	}
	gps := DeriveGrantedPathSet(policy)

	want := resolveSymlink(filepath.Join(home, ".gitconfig"))
	if !containsPath(gps.Readable, want) {
		t.Errorf("git-integration's ~/.gitconfig must reach GrantedPathSet.Readable for Landlock; got %v", gps.Readable)
	}
	if containsPath(gps.Writable, want) {
		t.Errorf("git config is read-only and must not appear in Writable: %v", gps.Writable)
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

// TestDeriveGrantedPathSet_DenyWinsSubtree ensures that denying a directory also
// excludes its children from Writable and Readable (tree-based deny semantics).
func TestDeriveGrantedPathSet_DenyWinsSubtree(t *testing.T) {
	// Arrange
	parent := t.TempDir()
	child := filepath.Join(parent, "nested", "file")
	if err := os.MkdirAll(filepath.Dir(child), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(child, []byte("secret"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	policy := Policy{
		ExtraDenied:  []string{parent},
		ExtraWritable: []string{child},
		ExtraReadable: []string{child},
		Guards:       []string{},
	}

	// Act
	gps := DeriveGrantedPathSet(policy)

	// Assert: child is inside the denied parent subtree — must not appear in either set.
	if containsPath(gps.Writable, child) {
		t.Errorf("child of denied dir %q must not appear in Writable; Writable=%v", parent, gps.Writable)
	}
	if containsPath(gps.Readable, child) {
		t.Errorf("child of denied dir %q must not appear in Readable; Readable=%v", parent, gps.Readable)
	}
	if !containsPath(gps.Denied, parent) {
		t.Errorf("denied parent %q must appear in Denied: %v", parent, gps.Denied)
	}
}

// TestDeriveGrantedPathSet_DenyWinsSubtree_SiblingUnaffected confirms that a
// sibling path (same prefix but different directory) is NOT excluded by a deny.
func TestDeriveGrantedPathSet_DenyWinsSubtree_SiblingUnaffected(t *testing.T) {
	// Arrange
	base := t.TempDir()
	denied := filepath.Join(base, "secrets")
	sibling := filepath.Join(base, "secrets-backup") // same prefix, different directory
	for _, d := range []string{denied, sibling} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatalf("MkdirAll %s: %v", d, err)
		}
	}

	policy := Policy{
		ExtraDenied:  []string{denied},
		ExtraReadable: []string{sibling},
		Guards:       []string{},
	}

	// Act
	gps := DeriveGrantedPathSet(policy)

	// Assert: sibling shares a name prefix but is a separate directory — must be readable.
	if !containsPath(gps.Readable, sibling) {
		t.Errorf("sibling %q must remain in Readable after denying %q; Readable=%v", sibling, denied, gps.Readable)
	}
}

// TestIsUnderDeniedTree_ExactMatch verifies exact-path denial.
func TestIsUnderDeniedTree_ExactMatch(t *testing.T) {
	denied := map[string]bool{"/home/user/.aws": true}

	if !isUnderDeniedTree("/home/user/.aws", denied) {
		t.Error("exact match must be denied")
	}
}

// TestIsUnderDeniedTree_ChildDenied verifies child-path denial.
func TestIsUnderDeniedTree_ChildDenied(t *testing.T) {
	denied := map[string]bool{"/home/user/.aws": true}

	if !isUnderDeniedTree("/home/user/.aws/credentials", denied) {
		t.Error("child of denied dir must be denied")
	}
	if !isUnderDeniedTree("/home/user/.aws/config", denied) {
		t.Error("child of denied dir must be denied")
	}
}

// TestIsUnderDeniedTree_SiblingAllowed verifies sibling paths are not denied.
func TestIsUnderDeniedTree_SiblingAllowed(t *testing.T) {
	denied := map[string]bool{"/home/user/.aws": true}

	if isUnderDeniedTree("/home/user/.aws-backup", denied) {
		t.Error("sibling with shared prefix must NOT be denied")
	}
	if isUnderDeniedTree("/home/user/.awsome", denied) {
		t.Error("path with shared prefix but different suffix must NOT be denied")
	}
}

// TestIsUnderDeniedTree_UnrelatedAllowed verifies unrelated paths are not denied.
func TestIsUnderDeniedTree_UnrelatedAllowed(t *testing.T) {
	denied := map[string]bool{"/home/user/.aws": true}

	if isUnderDeniedTree("/home/user/.ssh", denied) {
		t.Error("unrelated path must NOT be denied")
	}
	if isUnderDeniedTree("/tmp/work", denied) {
		t.Error("unrelated path must NOT be denied")
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
