package guards_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/testutil"
	"github.com/jskswamy/aide/pkg/seatbelt"
	"github.com/jskswamy/aide/pkg/seatbelt/guards"
)

func TestFilesystem_WritablePaths(t *testing.T) {
	tmp := t.TempDir()
	dir1 := filepath.Join(tmp, "writable1")
	dir2 := filepath.Join(tmp, "writable2")
	if err := os.Mkdir(dir1, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(dir2, 0o755); err != nil {
		t.Fatal(err)
	}

	g := guards.FilesystemGuard()
	ctx := &seatbelt.Context{
		ProjectRoot: dir1,
		RuntimeDir:  dir2,
	}

	if g.Name() != "filesystem" {
		t.Errorf("expected Name() = %q, got %q", "filesystem", g.Name())
	}

	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	if !strings.Contains(output, "(allow file-read* file-write*") {
		t.Error("expected allow file-read* file-write* block")
	}
	if !strings.Contains(output, `(subpath "`+dir1+`")`) {
		t.Errorf("expected subpath for %s", dir1)
	}
	if !strings.Contains(output, `(subpath "`+dir2+`")`) {
		t.Errorf("expected subpath for %s", dir2)
	}
}

func TestFilesystem_ScopedReadablePaths(t *testing.T) {
	g := guards.FilesystemGuard()
	ctx := &seatbelt.Context{
		HomeDir: "/Users/testuser",
	}

	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	// Should have narrow baseline paths, not broad $HOME read
	if !strings.Contains(output, "(allow file-read*") {
		t.Error("expected allow file-read* block")
	}
	if !strings.Contains(output, `"/Users/testuser/.config/aide"`) {
		t.Error("expected .config/aide subpath")
	}
}

func TestFilesystem_DeniedPaths(t *testing.T) {
	tmp := t.TempDir()
	file1 := filepath.Join(tmp, "secret.txt")
	if err := os.WriteFile(file1, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}

	g := guards.FilesystemGuard()
	ctx := &seatbelt.Context{
		ExtraDenied: []string{file1},
	}

	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	if !strings.Contains(output, "(deny file-read-data") {
		t.Error("expected deny file-read-data for denied path")
	}
	if !strings.Contains(output, "(deny file-write*") {
		t.Error("expected deny file-write* for denied path")
	}
	if !strings.Contains(output, `(literal "`+file1+`")`) {
		t.Errorf("expected literal for file %s", file1)
	}
}

func TestFilesystem_GlobExpansion(t *testing.T) {
	tmp := t.TempDir()
	// Create files matching a glob
	if err := os.WriteFile(filepath.Join(tmp, "a.env"), []byte("A=1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "b.env"), []byte("B=2"), 0o644); err != nil {
		t.Fatal(err)
	}

	g := guards.FilesystemGuard()
	ctx := &seatbelt.Context{
		ExtraDenied: []string{filepath.Join(tmp, "*.env")},
	}

	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	if !strings.Contains(output, "a.env") {
		t.Error("expected expanded glob to include a.env")
	}
	if !strings.Contains(output, "b.env") {
		t.Error("expected expanded glob to include b.env")
	}
}

func TestFilesystem_MixedConfig(t *testing.T) {
	tmp := t.TempDir()
	wdir := filepath.Join(tmp, "work")
	denied := filepath.Join(tmp, "secret.key")
	if err := os.Mkdir(wdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(denied, []byte("key"), 0o644); err != nil {
		t.Fatal(err)
	}

	g := guards.FilesystemGuard()
	ctx := &seatbelt.Context{
		ProjectRoot: wdir,
		HomeDir:     "/Users/testuser",
		ExtraDenied: []string{denied},
	}

	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	if !strings.Contains(output, "(allow file-read* file-write*") {
		t.Error("expected writable block")
	}
	if !strings.Contains(output, `(subpath "`+wdir+`")`) {
		t.Error("expected writable dir path")
	}
	if !strings.Contains(output, "(deny file-read-data") {
		t.Error("expected deny block")
	}
}

func TestFilesystemGuard_ExtraWritable(t *testing.T) {
	g := guards.FilesystemGuard()
	ctx := &seatbelt.Context{
		HomeDir:       "/Users/testuser",
		ProjectRoot:   "/project",
		ExtraWritable: []string{"/custom/writable"},
	}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	if !strings.Contains(output, `"/custom/writable"`) {
		t.Error("expected /custom/writable in filesystem guard output")
	}
	if !strings.Contains(output, "file-write*") {
		t.Error("expected file-write* rule for writable path")
	}
}

func TestFilesystemGuard_ExtraReadable(t *testing.T) {
	g := guards.FilesystemGuard()
	ctx := &seatbelt.Context{
		HomeDir:       "/Users/testuser",
		ProjectRoot:   "/project",
		ExtraReadable: []string{"/custom/readable"},
	}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	if !strings.Contains(output, `"/custom/readable"`) {
		t.Error("expected /custom/readable in filesystem guard output")
	}
	// ExtraReadable produces individual allow rules
	if !strings.Contains(output, "(allow file-read*") {
		t.Error("expected file-read* rule for extra readable path")
	}
}

// TestFilesystemGuard_ExtraReadable_ResolvesTwoHopChain reproduces the
// nix/home-manager case that motivated AIDE-46h: ~/.config/foo/bar is a
// symlink into the nix-store, which is itself a symlink to the user's
// nixos-config repo under $HOME. filepath.EvalSymlinks resolves the full
// chain transitively, and the rule must cover the final target.
func TestFilesystemGuard_ExtraReadable_ResolvesTwoHopChain(t *testing.T) {
	tmp := testutil.CanonicalTempDir(t)
	home := filepath.Join(tmp, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	// Final target — like ~/nixos-config/.../config.yaml
	finalTarget := filepath.Join(home, "nixos-config", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(finalTarget), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(finalTarget, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	// Intermediate hop — like /nix/store/.../config.yaml
	intermediate := filepath.Join(tmp, "nix-store-fake", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(intermediate), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(finalTarget, intermediate); err != nil {
		t.Fatal(err)
	}
	// Declared path — like ~/.config/bd/config.yaml
	declared := filepath.Join(home, ".config", "bd", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(declared), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(intermediate, declared); err != nil {
		t.Fatal(err)
	}

	g := guards.FilesystemGuard()
	output := renderTestRules(g.Rules(&seatbelt.Context{
		HomeDir:       home,
		ProjectRoot:   filepath.Join(home, "project"),
		ExtraReadable: []string{declared},
	}).Rules)

	if !strings.Contains(output, finalTarget) {
		t.Errorf("expected rule covering FINAL resolved target %q (after two hops) in output:\n%s", finalTarget, output)
	}
}

// TestFilesystemGuard_ExtraReadable_MissingPathDoesNotCrash locks in the
// graceful-fallback contract. A capability may declare a path that does
// not yet exist on disk (cache dir created on first run). EvalSymlinks
// errors with ENOENT; the guard must still emit the literal rule rather
// than crashing or skipping the path entirely.
func TestFilesystemGuard_ExtraReadable_MissingPathDoesNotCrash(t *testing.T) {
	home := testutil.CanonicalTempDir(t)
	missing := filepath.Join(home, "does", "not", "exist.yaml")

	g := guards.FilesystemGuard()
	output := renderTestRules(g.Rules(&seatbelt.Context{
		HomeDir:       home,
		ProjectRoot:   filepath.Join(home, "project"),
		ExtraReadable: []string{missing},
	}).Rules)

	if !strings.Contains(output, missing) {
		t.Errorf("expected literal rule for non-existent declared path %q in output:\n%s", missing, output)
	}
}

// TestFilesystemGuard_ExtraReadable_RejectsOutsideHomeWidening guards
// against silently broadening the sandbox. When a capability path's
// resolved target falls outside $HOME, the guard MUST NOT emit a
// widening rule — the user's intent is opt-in to the declared path, and
// outside-$HOME widening is reserved for the upstream escape hatch
// (AIDE-mu8). The literal rule still emits so the user sees a clear
// EPERM at runtime rather than confused behavior.
func TestFilesystemGuard_ExtraReadable_RejectsOutsideHomeWidening(t *testing.T) {
	tmp := testutil.CanonicalTempDir(t)
	home := filepath.Join(tmp, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(tmp, "outside-home", "secret.txt")
	if err := os.MkdirAll(filepath.Dir(outside), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outside, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(home, "escape-link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}

	g := guards.FilesystemGuard()
	output := renderTestRules(g.Rules(&seatbelt.Context{
		HomeDir:       home,
		ProjectRoot:   filepath.Join(home, "project"),
		ExtraReadable: []string{link},
	}).Rules)

	if !strings.Contains(output, link) {
		t.Errorf("expected literal rule for the declared symlink %q in output:\n%s", link, output)
	}
	if strings.Contains(output, outside) {
		t.Errorf("guard MUST NOT widen to outside-$HOME target %q (reserved for opt-in escape hatch); output:\n%s", outside, output)
	}
}

// TestFilesystemGuard_ExtraDenied_ResolvesSymlink covers the security-critical
// symmetric-resolution case. If never_allow or aide-secrets points at a path
// that happens to be a symlink, the kernel resolves it on write and the deny
// rule for the literal symlink alone would never fire — the resolved target
// would be writable through the symlink. Denies therefore widen to the
// resolved target regardless of where it lives (the user's intent is
// protection; there is no over-denial harm).
func TestFilesystemGuard_ExtraDenied_ResolvesSymlink(t *testing.T) {
	home := testutil.CanonicalTempDir(t)
	target := filepath.Join(home, "real-secret")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	link := filepath.Join(home, "secret-link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	g := guards.FilesystemGuard()
	ctx := &seatbelt.Context{
		HomeDir:     home,
		ProjectRoot: filepath.Join(home, "project"),
		ExtraDenied: []string{link},
	}
	output := renderTestRules(g.Rules(ctx).Rules)

	if !strings.Contains(output, "(deny file-write* "+`(subpath "`+target+`"))`) &&
		!strings.Contains(output, "(deny file-write* "+`(literal "`+target+`"))`) {
		t.Errorf("expected deny rule covering resolved target %q in output:\n%s", target, output)
	}
	if !strings.Contains(output, "(deny file-read-data "+`(subpath "`+target+`"))`) &&
		!strings.Contains(output, "(deny file-read-data "+`(literal "`+target+`"))`) {
		t.Errorf("expected deny-read-data rule covering resolved target %q in output:\n%s", target, output)
	}
}

// TestFilesystemGuard_ExtraWritable_ResolvesSymlinkUnderHome covers the
// write-side analogue of the read-side symlink contract. Capability writable
// paths bundle into the single (require-any ...) block alongside project /
// runtime / temp roots; the resolved target must appear in that block so
// kernel-resolved write paths actually match.
func TestFilesystemGuard_ExtraWritable_ResolvesSymlinkUnderHome(t *testing.T) {
	home := testutil.CanonicalTempDir(t)
	target := filepath.Join(home, "real-write")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	link := filepath.Join(home, ".local", "writable-link")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatalf("mkdir link parent: %v", err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	g := guards.FilesystemGuard()
	ctx := &seatbelt.Context{
		HomeDir:       home,
		ProjectRoot:   filepath.Join(home, "project"),
		ExtraWritable: []string{link},
	}
	output := renderTestRules(g.Rules(ctx).Rules)

	if !strings.Contains(output, `"`+link+`"`) {
		t.Errorf("expected literal rule for symlink path %q in output:\n%s", link, output)
	}
	if !strings.Contains(output, `"`+target+`"`) {
		t.Errorf("expected resolved-target rule for %q (kernel-matched on writes) in output:\n%s", target, output)
	}
}

// TestFilesystemGuard_ExtraReadable_ResolvesSymlinkUnderHome pins the symlink-
// resolution contract for capability/raw-config readable paths. macOS seatbelt
// matches file-read* policy on the kernel-resolved path, so a literal-only
// allow rule for a symlinked path never fires — the kernel asks the policy
// about the resolved target instead. When the resolved target is under $HOME
// (the common home-manager / dotfiles-repo pattern), the guard must emit an
// additional rule covering it.
func TestFilesystemGuard_ExtraReadable_ResolvesSymlinkUnderHome(t *testing.T) {
	home := testutil.CanonicalTempDir(t)
	target := filepath.Join(home, "real", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("mkdir target dir: %v", err)
	}
	if err := os.WriteFile(target, []byte("ok"), 0o644); err != nil {
		t.Fatalf("write target: %v", err)
	}
	link := filepath.Join(home, ".config", "foo", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatalf("mkdir link parent: %v", err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	g := guards.FilesystemGuard()
	ctx := &seatbelt.Context{
		HomeDir:       home,
		ProjectRoot:   filepath.Join(home, "project"),
		ExtraReadable: []string{link},
	}
	output := renderTestRules(g.Rules(ctx).Rules)

	if !strings.Contains(output, `"`+link+`"`) {
		t.Errorf("expected literal rule for symlink path %q in output:\n%s", link, output)
	}
	if !strings.Contains(output, `"`+target+`"`) {
		t.Errorf("expected resolved-target rule for %q (the path macOS kernel matches) in output:\n%s", target, output)
	}
}

func TestFilesystemGuard_ScopedHomeReads(t *testing.T) {
	g := guards.FilesystemGuard()
	ctx := &seatbelt.Context{
		HomeDir:     "/Users/testuser",
		ProjectRoot: "/project",
	}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	// Should NOT have broad $HOME read
	if strings.Contains(output, `(subpath "/Users/testuser")`) &&
		!strings.Contains(output, `(subpath "/Users/testuser/`) {
		t.Error("should NOT have broad $HOME subpath read")
	}

	// Should have narrow baseline paths
	narrowPaths := []string{
		`"/Users/testuser/.config/aide"`,
		`"/Users/testuser/.local/share/aide"`,
		`"/Users/testuser/.cache"`,
		`"/Users/testuser/Library/Caches"`,
	}
	for _, p := range narrowPaths {
		if !strings.Contains(output, p) {
			t.Errorf("expected narrow baseline path %s in output", p)
		}
	}

	// Should NOT have broad dev paths
	broadPaths := []string{
		`"/Users/testuser/.ssh"`,
		`"/Users/testuser/.cargo"`,
		`"/Users/testuser/.gnupg"`,
	}
	for _, p := range broadPaths {
		if strings.Contains(output, p) {
			t.Errorf("should NOT have broad path %s in output", p)
		}
	}

	// Should NOT have dotfile regex
	if strings.Contains(output, "regex") {
		t.Error("should NOT have regex rule for home dotfiles")
	}

	// Project root should still be writable
	if !strings.Contains(output, `"/project"`) {
		t.Error("expected project root in writable paths")
	}
}

func TestGuard_Filesystem_Metadata(t *testing.T) {
	g := guards.FilesystemGuard()

	if g.Name() != "filesystem" {
		t.Errorf("expected Name() = %q, got %q", "filesystem", g.Name())
	}
	if g.Type() != "always" {
		t.Errorf("expected Type() = %q, got %q", "always", g.Type())
	}
	if g.Description() == "" {
		t.Error("expected non-empty Description()")
	}
}

func TestGuard_Filesystem_CtxPaths(t *testing.T) {
	tmp := t.TempDir()
	project := filepath.Join(tmp, "project")
	runtime := filepath.Join(tmp, "runtime")
	denied := filepath.Join(tmp, "secret.key")

	for _, d := range []string{project, runtime} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(denied, []byte("key"), 0o644); err != nil {
		t.Fatal(err)
	}

	g := guards.FilesystemGuard()
	ctx := &seatbelt.Context{
		ProjectRoot: project,
		HomeDir:     "/Users/testuser",
		RuntimeDir:  runtime,
		ExtraDenied: []string{denied},
	}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	if !strings.Contains(output, "(allow file-read* file-write*") {
		t.Error("expected writable block for ProjectRoot/RuntimeDir")
	}
	if !strings.Contains(output, `(subpath "`+project+`")`) {
		t.Errorf("expected ProjectRoot %s in output", project)
	}
	if !strings.Contains(output, "(deny file-read-data") {
		t.Error("expected deny block for ExtraDenied")
	}
	if !strings.Contains(output, `(literal "`+denied+`")`) {
		t.Errorf("expected denied path %s in output", denied)
	}
}

func TestFilesystem_ExtraAllow(t *testing.T) {
	g := guards.FilesystemGuard()
	ctx := &seatbelt.Context{
		HomeDir:     "/Users/test",
		ProjectRoot: "/tmp/project",
		GOOS:        "darwin",
		ExtraAllow:  []string{"mach-lookup", "iokit-open", "signal"},
	}
	result := g.Rules(ctx)
	profile := renderTestRules(result.Rules)

	expected := []string{
		"(allow mach-lookup)",
		"(allow iokit-open)",
		"(allow signal)",
	}
	for _, want := range expected {
		if !strings.Contains(profile, want) {
			t.Errorf("expected %q in profile, got:\n%s", want, profile)
		}
	}
}

func TestFilesystem_ExtraAllow_Empty(t *testing.T) {
	g := guards.FilesystemGuard()
	ctx := &seatbelt.Context{
		HomeDir:     "/Users/test",
		ProjectRoot: "/tmp/project",
		GOOS:        "darwin",
	}
	result := g.Rules(ctx)
	profile := renderTestRules(result.Rules)

	if strings.Contains(profile, "Non-filesystem operations") {
		t.Error("should not emit non-filesystem section when ExtraAllow is empty")
	}
}

func TestFilesystemGuard_NarrowBaseline(t *testing.T) {
	g := guards.FilesystemGuard()
	ctx := &seatbelt.Context{
		HomeDir:     "/Users/testuser",
		ProjectRoot: "/project",
	}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	// Should have narrow baseline reads
	allowedPaths := []struct {
		path    string
		kind    string // "literal" or "subpath"
	}{
		{`(subpath "/Users/testuser/.config/aide")`, "aide config"},
		{`(subpath "/Users/testuser/.local/share/aide")`, "aide data"},
		{`(subpath "/Users/testuser/.cache")`, "cache dir"},
		{`(subpath "/Users/testuser/Library/Caches")`, "Library Caches"},
	}
	for _, ap := range allowedPaths {
		if !strings.Contains(output, ap.path) {
			t.Errorf("expected %s: %s", ap.kind, ap.path)
		}
	}

	// Should NOT have broad paths that are now removed
	broadPaths := []struct {
		path string
		desc string
	}{
		{`"/Users/testuser/.config"`, "broad .config"},
		{`"/Users/testuser/.local"`, "broad .local"},
		{`"/Users/testuser/.ssh"`, "broad .ssh"},
		{`"/Users/testuser/.cargo"`, "broad .cargo"},
		{`"/Users/testuser/.rustup"`, "broad .rustup"},
		{`"/Users/testuser/go"`, "broad go"},
		{`"/Users/testuser/.pyenv"`, "broad .pyenv"},
		{`"/Users/testuser/.rbenv"`, "broad .rbenv"},
		{`"/Users/testuser/.sdkman"`, "broad .sdkman"},
		{`"/Users/testuser/.gradle"`, "broad .gradle"},
		{`"/Users/testuser/.m2"`, "broad .m2"},
		{`"/Users/testuser/.gnupg"`, "broad .gnupg"},
		{`"/Users/testuser/.nix-profile"`, "broad .nix-profile"},
		{`"/Users/testuser/.nix-defexpr"`, "broad .nix-defexpr"},
		{`"/Users/testuser/Library/Keychains"`, "broad Library/Keychains"},
		{`"/Users/testuser/Library/Preferences"`, "broad Library/Preferences"},
	}
	for _, bp := range broadPaths {
		if strings.Contains(output, bp.path) {
			t.Errorf("should NOT have %s: %s", bp.desc, bp.path)
		}
	}

	// Should NOT have dotfile regex
	if strings.Contains(output, "regex") {
		t.Error("should NOT have regex rule for home dotfiles")
	}

	// Should still have home directory traversal
	if !strings.Contains(output, `(literal "/Users/testuser")`) {
		t.Error("expected home directory listing literal")
	}
	if !strings.Contains(output, `(allow file-read-metadata`) {
		t.Error("expected home metadata traversal")
	}
}
