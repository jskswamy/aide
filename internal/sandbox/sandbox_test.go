package sandbox

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt/guards"
)

func TestDefaultPolicy_GuardNames(t *testing.T) {
	policy := DefaultPolicy(Paths{ProjectRoot: "/tmp/proj", RuntimeDir: "/tmp/rt", TempDir: "/tmp"}, nil)

	expected := guards.DefaultGuardNames()
	if len(policy.Guards) != len(expected) {
		t.Fatalf("expected %d guards, got %d: %v", len(expected), len(policy.Guards), policy.Guards)
	}
	for i, name := range expected {
		if policy.Guards[i] != name {
			t.Errorf("Guards[%d]: expected %q, got %q", i, name, policy.Guards[i])
		}
	}
}

func TestDefaultPolicy_Paths(t *testing.T) {
	projectRoot := "/tmp/myproject"
	runtimeDir := "/tmp/aide-12345"
	tempDir := "/tmp"

	policy := DefaultPolicy(Paths{ProjectRoot: projectRoot, RuntimeDir: runtimeDir, TempDir: tempDir}, nil)

	if policy.ProjectRoot != projectRoot {
		t.Errorf("expected ProjectRoot=%q, got %q", projectRoot, policy.ProjectRoot)
	}
	if policy.RuntimeDir != runtimeDir {
		t.Errorf("expected RuntimeDir=%q, got %q", runtimeDir, policy.RuntimeDir)
	}
	if policy.TempDir != tempDir {
		t.Errorf("expected TempDir=%q, got %q", tempDir, policy.TempDir)
	}
}

func TestDefaultPolicy_NetworkIsOutbound(t *testing.T) {
	policy := DefaultPolicy(Paths{ProjectRoot: "/tmp/proj", RuntimeDir: "/tmp/rt", TempDir: "/tmp"}, nil)

	if policy.Network != NetworkOutbound {
		t.Errorf("expected Network=%q, got %q", NetworkOutbound, policy.Network)
	}
}

func TestDefaultPolicy_AllowSubprocess(t *testing.T) {
	policy := DefaultPolicy(Paths{ProjectRoot: "/tmp/proj", RuntimeDir: "/tmp/rt", TempDir: "/tmp"}, nil)

	if !policy.AllowSubprocess {
		t.Error("expected AllowSubprocess=true, got false")
	}
}

func TestDefaultPolicy_CleanEnv(t *testing.T) {
	policy := DefaultPolicy(Paths{ProjectRoot: "/tmp/proj", RuntimeDir: "/tmp/rt", TempDir: "/tmp"}, nil)

	if policy.CleanEnv {
		t.Error("expected CleanEnv=false, got true")
	}
}

func TestDefaultPolicy_Env(t *testing.T) {
	env := []string{"PATH=/usr/bin", "HOME=/home/user"}
	policy := DefaultPolicy(Paths{ProjectRoot: "/tmp/proj", RuntimeDir: "/tmp/rt", TempDir: "/tmp"}, env)

	if len(policy.Env) != 2 {
		t.Fatalf("expected 2 env vars, got %d", len(policy.Env))
	}
	if policy.Env[0] != "PATH=/usr/bin" {
		t.Errorf("expected Env[0]=%q, got %q", "PATH=/usr/bin", policy.Env[0])
	}
}

func TestNoopSandbox_Apply_ReturnsNil(t *testing.T) {
	s := &noopSandbox{}
	cmd := exec.Command("echo", "hello")
	policy := DefaultPolicy(Paths{ProjectRoot: "/tmp/proj", RuntimeDir: "/tmp/rt", TempDir: "/tmp"}, nil)

	err := s.Apply(cmd, policy, "/tmp/rt")
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
}

func TestNewSandbox_ReturnsNonNil(t *testing.T) {
	s := NewSandbox()
	if s == nil {
		t.Error("expected NewSandbox() to return non-nil Sandbox")
	}
}

func TestNetworkModeConstants(t *testing.T) {
	if NetworkOutbound != "outbound" {
		t.Errorf("expected NetworkOutbound=%q, got %q", "outbound", NetworkOutbound)
	}
	if NetworkNone != "none" {
		t.Errorf("expected NetworkNone=%q, got %q", "none", NetworkNone)
	}
	if NetworkUnrestricted != "unrestricted" {
		t.Errorf("expected NetworkUnrestricted=%q, got %q", "unrestricted", NetworkUnrestricted)
	}
}

func TestDefaultPolicy_NilAgentModule(t *testing.T) {
	policy := DefaultPolicy(Paths{ProjectRoot: "/tmp/proj", RuntimeDir: "/tmp/rt", TempDir: "/tmp"}, nil)

	if policy.AgentModule != nil {
		t.Error("expected AgentModule=nil for default policy")
	}
}

func TestDefaultPolicy_EmptyExtraDenied(t *testing.T) {
	policy := DefaultPolicy(Paths{ProjectRoot: "/tmp/proj", RuntimeDir: "/tmp/rt", TempDir: "/tmp"}, nil)

	if len(policy.ExtraDenied) != 0 {
		t.Errorf("expected empty ExtraDenied, got %v", policy.ExtraDenied)
	}
}

// TestResolveSymlink_NonexistentPath confirms that paths which do not exist
// are still surfaced as their cleaned form — Landlock applies rules at boot,
// so a runtime dir that gets created later must still receive a rule.
func TestResolveSymlink_NonexistentPath(t *testing.T) {
	got := resolveSymlink("/definitely/does/not/exist")
	if got != "/definitely/does/not/exist" {
		t.Errorf("resolveSymlink(missing) = %q, want cleaned input (must never drop guard-vouched paths)", got)
	}
}

// TestResolveSymlink_PreservesPathOnEACCES pins the Greptile P2 fix: when
// EvalSymlinks fails for a non-ENOENT reason (here, EACCES on a parent dir
// component the test user cannot traverse), the helper must fall back to
// filepath.Clean rather than returning "". Returning "" silently dropped
// guard-vouched grants from the Landlock allow-list and surfaced later as a
// bare EACCES inside the agent with no diagnostic.
func TestResolveSymlink_PreservesPathOnEACCES(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory traversal permissions; this test depends on EACCES")
	}
	parent := filepath.Join(t.TempDir(), "no-traverse")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatalf("setup parent: %v", err)
	}
	target := filepath.Join(parent, "child")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatalf("setup target: %v", err)
	}
	// Strip search permission on parent so EvalSymlinks fails with EACCES
	// when traversing into it from the test process.
	if err := os.Chmod(parent, 0o000); err != nil {
		t.Fatalf("chmod parent: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0o700) })

	got := resolveSymlink(target)
	if got == "" {
		t.Fatalf("resolveSymlink(%q) returned empty; must fall back to cleaned input on EACCES (else guard-vouched grants are silently dropped)", target)
	}
	if got != filepath.Clean(target) {
		t.Errorf("resolveSymlink(%q) = %q, want %q (cleaned input)", target, got, filepath.Clean(target))
	}
}

// TestResolveSymlinkForDeny_StillFallsBack pins the equivalence with
// resolveSymlink — the deny-side helper has always had this behaviour; the
// fix made the allow-side match. If they diverge again, deny entries could
// silently drop and a later allow rule could pierce a protected location.
func TestResolveSymlinkForDeny_StillFallsBack(t *testing.T) {
	got := resolveSymlinkForDeny("/no/such/path")
	if got != "/no/such/path" {
		t.Errorf("resolveSymlinkForDeny(missing) = %q, want cleaned input", got)
	}
}

func TestExpandGlobs_PreservesUnmatchedLiteralWithBrackets(t *testing.T) {
	dir := t.TempDir()
	bracketFile := filepath.Join(dir, "prod_[1]")
	sibling := filepath.Join(dir, "prod_1")
	for _, f := range []string{bracketFile, sibling} {
		if err := os.WriteFile(f, []byte("secret"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	got := expandGlobs([]string{bracketFile})
	assertContains(t, got, bracketFile, "expandGlobs must preserve an existing literal path even when a glob sibling matches")
}

func TestSplitAroundDenied_AncestorSymlinkCycleDoesNotHang(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ssh := filepath.Join(root, ".ssh")
	if err := os.MkdirAll(ssh, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"id_ed25519", "prod_db"} {
		if err := os.WriteFile(filepath.Join(ssh, f), []byte("k"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// Create an ancestor symlink loop: ~/.ssh/loop -> ~/.ssh
	if err := os.Symlink(ssh, filepath.Join(ssh, "loop")); err != nil {
		t.Fatal(err)
	}

	got, splitDirs := splitAroundDenied([]string{ssh}, []string{filepath.Join(ssh, "prod_db")}, map[string]string{ssh: "ssh"})
	want := []string{filepath.Join(ssh, "id_ed25519")}
	if !slices.Equal(got, want) {
		t.Errorf("splitAroundDenied() = %v, want %v", got, want)
	}
	if !slices.Equal(splitDirs, []string{ssh}) {
		t.Errorf("splitDirs = %v, want [%s]", splitDirs, ssh)
	}
}

func TestSplitAroundDenied(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ssh := filepath.Join(root, ".ssh")
	for _, f := range []string{"id_ed25519", "config", "prod_db", "prod/deploy", "prod/../team/key", "team/prod_key"} {
		p := filepath.Join(ssh, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("k"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	other := filepath.Join(root, "other")
	denied := []string{filepath.Join(ssh, "prod_db"), filepath.Join(ssh, "prod"), filepath.Join(ssh, "team", "prod_key")}
	origin := map[string]string{ssh: "ssh"}

	got, splitDirs := splitAroundDenied([]string{ssh, other}, denied, origin)

	want := []string{
		filepath.Join(ssh, "config"),
		filepath.Join(ssh, "id_ed25519"),
		filepath.Join(ssh, "team", "key"),
		other,
	}
	if !slices.Equal(got, want) {
		t.Fatalf("splitAroundDenied() = %v, want %v", got, want)
	}
	wantSplitDirs := []string{ssh, filepath.Join(ssh, "team")}
	if !slices.Equal(splitDirs, wantSplitDirs) {
		t.Errorf("splitDirs = %v, want %v", splitDirs, wantSplitDirs)
	}
	if origin[filepath.Join(ssh, "team", "key")] != "ssh" {
		t.Errorf("split entries should inherit the parent grant's origin, got %q", origin[filepath.Join(ssh, "team", "key")])
	}
}

func TestSplitAroundDenied_ExcludesSymlinkEscapingGrantAndResolvesInTreeAlias(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	allowed := filepath.Join(root, "allowed")
	privateDir := filepath.Join(root, "private")
	for _, f := range []string{
		filepath.Join(allowed, "ok.txt"),
		filepath.Join(allowed, "blocked"),
		filepath.Join(allowed, "sub", "keep.txt"),
		filepath.Join(allowed, "sub", "secret"),
		filepath.Join(privateDir, "outside.txt"),
	} {
		if err := os.MkdirAll(filepath.Dir(f), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// Symlink inside allowed pointing outside allowed -> must not be granted.
	escapeLink := filepath.Join(allowed, "escape-link")
	if err := os.Symlink(privateDir, escapeLink); err != nil {
		t.Fatal(err)
	}
	// Symlink inside allowed pointing to in-tree subdir containing a denied descendant ->
	// must not grant the whole subdir and restore the denied descendant.
	aliasLink := filepath.Join(allowed, "alias-sub")
	if err := os.Symlink(filepath.Join(allowed, "sub"), aliasLink); err != nil {
		t.Fatal(err)
	}

	denied := []string{
		filepath.Join(allowed, "blocked"),
		filepath.Join(allowed, "sub", "secret"),
	}
	origin := map[string]string{allowed: "ssh"}

	got, _ := splitAroundDenied([]string{allowed}, denied, origin)

	for _, p := range got {
		if p == escapeLink || p == privateDir || strings.HasPrefix(p, privateDir+"/") {
			t.Errorf("splitAroundDenied granted out-of-tree symlink target %q: %v", p, got)
		}
		if p == aliasLink || p == filepath.Join(allowed, "sub") || p == filepath.Join(allowed, "sub", "secret") {
			t.Errorf("splitAroundDenied restored denied descendant via in-tree symlink %q: %v", p, got)
		}
	}
	assertContains(t, got, filepath.Join(allowed, "ok.txt"), "ok.txt should remain readable")
	assertContains(t, got, filepath.Join(allowed, "sub", "keep.txt"), "sub/keep.txt should remain readable")
}

// helper
func assertContains(t *testing.T, slice []string, item string, msg string) {
	t.Helper()
	for _, s := range slice {
		if s == item {
			return
		}
	}
	t.Errorf("%s: %q not found in %v", msg, item, slice)
}

// Capability read paths may be globs or absent on a given host (e.g. the
// gpu capability's /opt/rocm* and /sys/class/kfd): unmatched globs must
// drop silently, absent plain paths must warn and drop.
func TestReadablePaths_UnmatchedGlobSilentMissingPlainWarns(t *testing.T) {
	dir := t.TempDir()
	present := filepath.Join(dir, "present")
	if err := os.Mkdir(present, 0o755); err != nil {
		t.Fatal(err)
	}
	glob := filepath.Join(dir, "nomatch*")
	missing := filepath.Join(dir, "missing")

	var warnings []string
	kept := validateAndFilterPaths([]string{present, glob, missing}, &warnings)
	if want := []string{present, glob}; !slices.Equal(kept, want) {
		t.Errorf("validateAndFilterPaths = %v, want %v", kept, want)
	}
	if want := []string{"skipped: " + missing + " (not found)"}; !slices.Equal(warnings, want) {
		t.Errorf("warnings = %v, want %v", warnings, want)
	}
	if got := expandGlobs(kept); !slices.Equal(got, []string{present}) {
		t.Errorf("expandGlobs = %v, want [%s]", got, present)
	}
}
