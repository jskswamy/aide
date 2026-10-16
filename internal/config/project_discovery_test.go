package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/testutil"
)

func gitRun(t *testing.T, dir string, args ...string) { testutil.Git(t, dir, args...) }

func worktreeFixture(t *testing.T) (mainDir, wtDir string) { return testutil.GitWorktree(t) }

// realTempDir is t.TempDir with symlinks resolved, so paths compare
// equal to what git reports.
func realTempDir(t *testing.T) string {
	t.Helper()
	d, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestDiscoverProject_MainCheckoutAppliesItsFileOnce(t *testing.T) {
	mainDir, _ := worktreeFixture(t)
	writeFile(t, filepath.Join(mainDir, ".aide.yaml"), "capabilities: [go]\n")

	d := DiscoverProject(mainDir)
	if d.Err != nil {
		t.Fatal(d.Err)
	}
	if d.LocalPath != filepath.Join(mainDir, ".aide.yaml") {
		t.Errorf("LocalPath = %q", d.LocalPath)
	}
	if d.MainCheckoutRoot != "" || d.MainPath != "" {
		t.Errorf("main checkout must not inherit from itself: %+v", d)
	}
}

func TestDiscoverProject_LinkedWorktreeFindsMainCheckout(t *testing.T) {
	mainDir, wtDir := worktreeFixture(t)
	writeFile(t, filepath.Join(mainDir, ".aide.yaml"), "capabilities: [go]\n")
	sub := filepath.Join(wtDir, "pkg", "deep")
	if err := os.MkdirAll(sub, 0o750); err != nil {
		t.Fatal(err)
	}

	d := DiscoverProject(sub)
	if d.Err != nil {
		t.Fatal(d.Err)
	}
	if d.CheckoutRoot != wtDir {
		t.Errorf("CheckoutRoot = %q, want %q", d.CheckoutRoot, wtDir)
	}
	if d.MainCheckoutRoot != mainDir {
		t.Errorf("MainCheckoutRoot = %q, want %q", d.MainCheckoutRoot, mainDir)
	}
	if d.MainPath != filepath.Join(mainDir, ".aide.yaml") {
		t.Errorf("MainPath = %q", d.MainPath)
	}
	if d.LocalPath != "" {
		t.Errorf("LocalPath = %q, want none", d.LocalPath)
	}
	if got := d.LocalWritePath(sub); got != filepath.Join(wtDir, ".aide.yaml") {
		t.Errorf("LocalWritePath = %q, want the worktree root's file", got)
	}
}

// The main checkout need not be a parent of the worktree: discovery
// asks git, it does not walk filesystem parents.
func TestDiscoverProject_MissingMainFileIsNormal(t *testing.T) {
	_, wtDir := worktreeFixture(t)
	d := DiscoverProject(wtDir)
	if d.Err != nil {
		t.Fatalf("a main checkout without .aide.yaml is normal: %v", d.Err)
	}
	if d.MainPath != "" {
		t.Errorf("MainPath = %q, want none", d.MainPath)
	}
}

func TestDiscoverProject_DeletedMainCheckoutIsADiagnostic(t *testing.T) {
	mainDir, wtDir := worktreeFixture(t)
	if err := os.RemoveAll(mainDir); err != nil {
		t.Fatal(err)
	}
	d := DiscoverProject(wtDir)
	if !errors.Is(d.Err, ErrMainCheckoutUnavailable) {
		t.Fatalf("Err = %v, want ErrMainCheckoutUnavailable", d.Err)
	}
	msg := d.Err.Error()
	for _, want := range []string{mainDir, "git worktree repair", "git worktree prune", "--ignore-project-config"} {
		if !strings.Contains(msg, want) {
			t.Errorf("diagnostic %q does not mention %q", msg, want)
		}
	}
}

// A worktree of a bare repository has no main checkout file to
// inherit.
func TestDiscoverProject_BareRepositoryInheritsNothing(t *testing.T) {
	mainDir, _ := worktreeFixture(t)
	base := filepath.Dir(mainDir)
	gitRun(t, base, "clone", "-q", "--bare", "main", "bare.git")
	bwt := filepath.Join(base, "bare-wt")
	gitRun(t, filepath.Join(base, "bare.git"), "worktree", "add", "-q", bwt)
	writeFile(t, filepath.Join(base, "bare.git", ".aide.yaml"), "capabilities: [aws]\n")

	d := DiscoverProject(bwt)
	if d.Err != nil {
		t.Fatal(d.Err)
	}
	if d.MainCheckoutRoot != "" || d.MainPath != "" {
		t.Errorf("bare repository must contribute nothing: %+v", d)
	}
}

// A submodule is its own repository; it never inherits the
// superproject's .aide.yaml.
func TestDiscoverProject_SubmoduleDoesNotInheritSuperproject(t *testing.T) {
	mainDir, _ := worktreeFixture(t)
	base := filepath.Dir(mainDir)
	gitRun(t, base, "init", "-q", "lib")
	gitRun(t, filepath.Join(base, "lib"), "commit", "-q", "--allow-empty", "-m", "lib")
	gitRun(t, mainDir, "-c", "protocol.file.allow=always", "submodule", "add", "-q", filepath.Join(base, "lib"), "lib")
	writeFile(t, filepath.Join(mainDir, ".aide.yaml"), "capabilities: [aws]\n")

	d := DiscoverProject(filepath.Join(mainDir, "lib"))
	if d.Err != nil {
		t.Fatal(d.Err)
	}
	if d.LocalPath != "" || d.MainPath != "" || d.MainCheckoutRoot != "" {
		t.Errorf("submodule must not see the superproject file: %+v", d)
	}
}

func TestDiscoverProject_OutsideGit(t *testing.T) {
	dir := realTempDir(t)
	writeFile(t, filepath.Join(dir, ".aide.yaml"), "agent: claude\n")
	sub := filepath.Join(dir, "a")
	if err := os.Mkdir(sub, 0o750); err != nil {
		t.Fatal(err)
	}
	d := DiscoverProject(sub)
	if d.LocalPath != filepath.Join(dir, ".aide.yaml") || d.CheckoutRoot != "" || d.Err != nil {
		t.Errorf("unexpected discovery outside git: %+v", d)
	}
}

func writeGlobal(t *testing.T) string {
	t.Helper()
	configDir := t.TempDir()
	writeFile(t, filepath.Join(configDir, "config.yaml"), "profiles:\n  work:\n    agent: claude\ndefault_profile: work\n")
	return configDir
}

func TestLoad_WorktreeLayersInheritedThenLocal(t *testing.T) {
	mainDir, wtDir := worktreeFixture(t)
	writeFile(t, filepath.Join(mainDir, ".aide.yaml"),
		"capabilities: [go, docker]\nenv:\n  A: main\n  B: main\nsandbox:\n  writable_extra: [build]\n")
	writeFile(t, filepath.Join(wtDir, ".aide.yaml"),
		"agent: codex\ncapabilities: [node]\nenv:\n  B: local\nsandbox:\n  readable_extra: [docs]\n")
	sub := filepath.Join(wtDir, "sub")
	if err := os.Mkdir(sub, 0o750); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(writeGlobal(t), sub)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.ProjectLayers) != 2 || cfg.ProjectLayers[0].Source != LayerInherited || cfg.ProjectLayers[1].Source != LayerLocal {
		t.Fatalf("layers = %+v", cfg.ProjectLayers)
	}
	po := cfg.ProjectOverride
	if strings.Join(po.Capabilities, ",") != "go,docker,node" {
		t.Errorf("Capabilities = %v", po.Capabilities)
	}
	if po.Agent != "codex" || po.Env["A"] != "main" || po.Env["B"] != "local" {
		t.Errorf("merge wrong: agent=%q env=%v", po.Agent, po.Env)
	}
	// Each file's relative paths are anchored to its own checkout.
	if got := po.Sandbox.WritableExtra; len(got) != 1 || got[0] != filepath.Join(mainDir, "build") {
		t.Errorf("inherited writable_extra = %v, want %s", got, filepath.Join(mainDir, "build"))
	}
	if got := po.Sandbox.ReadableExtra; len(got) != 1 || got[0] != filepath.Join(wtDir, "docs") {
		t.Errorf("local readable_extra = %v, want %s", got, filepath.Join(wtDir, "docs"))
	}
	if cfg.ProjectConfigPath != filepath.Join(wtDir, ".aide.yaml") {
		t.Errorf("ProjectConfigPath = %q, want the local file", cfg.ProjectConfigPath)
	}
}

func TestLoad_MainCheckoutLoadsItsFileOnce(t *testing.T) {
	mainDir, _ := worktreeFixture(t)
	writeFile(t, filepath.Join(mainDir, ".aide.yaml"), "capabilities: [go]\n")
	cfg, err := Load(writeGlobal(t), mainDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.ProjectLayers) != 1 || cfg.ProjectLayers[0].Source != LayerLocal {
		t.Fatalf("layers = %+v", cfg.ProjectLayers)
	}
	if strings.Join(cfg.ProjectOverride.Capabilities, ",") != "go" {
		t.Errorf("Capabilities = %v", cfg.ProjectOverride.Capabilities)
	}
}

// Every layer gets the structural check: an inherited file may not
// define a profile either.
func TestLoad_InheritedFileMayNotDefineAProfile(t *testing.T) {
	mainDir, wtDir := worktreeFixture(t)
	writeFile(t, filepath.Join(mainDir, ".aide.yaml"), "profiles:\n  evil:\n    agent: claude\n")
	_, err := Load(writeGlobal(t), wtDir)
	if !errors.Is(err, ErrProfileNotProjectScoped) {
		t.Fatalf("err = %v, want ErrProfileNotProjectScoped", err)
	}
}

func TestLoad_DeletedMainCheckoutIsRecordedNotFatal(t *testing.T) {
	mainDir, wtDir := worktreeFixture(t)
	writeFile(t, filepath.Join(wtDir, ".aide.yaml"), "capabilities: [node]\n")
	if err := os.RemoveAll(mainDir); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(writeGlobal(t), wtDir)
	if err != nil {
		t.Fatalf("Load: %v (the gate reports it, so --ignore-project-config can still launch)", err)
	}
	if !errors.Is(cfg.ProjectConfigErr, ErrMainCheckoutUnavailable) {
		t.Errorf("ProjectConfigErr = %v", cfg.ProjectConfigErr)
	}
}

func TestProjectConfigWriteTarget_Worktree(t *testing.T) {
	mainDir, wtDir := worktreeFixture(t)
	writeFile(t, filepath.Join(mainDir, ".aide.yaml"), "capabilities: [go]\n")

	local, err := ProjectConfigWriteTarget(wtDir, false)
	if err != nil || local != filepath.Join(wtDir, ".aide.yaml") {
		t.Errorf("local target = %q, %v; want the worktree's own file", local, err)
	}
	if got := FindProjectConfigForWrite(wtDir); got != local {
		t.Errorf("FindProjectConfigForWrite = %q, want %q", got, local)
	}
	main, err := ProjectConfigWriteTarget(wtDir, true)
	if err != nil || main != filepath.Join(mainDir, ".aide.yaml") {
		t.Errorf("--main target = %q, %v", main, err)
	}
	if _, err := ProjectConfigWriteTarget(mainDir, true); err == nil {
		t.Error("--main outside a linked worktree should fail")
	}
}

// A directory that forges a .git file pointing into its own fake
// administrative dir, whose commondir names a victim repository, is
// not a worktree of that repository: it must not inherit the victim's
// .aide.yaml.
func TestDiscoverProject_ForgedGitFileInheritsNothing(t *testing.T) {
	victim, _ := worktreeFixture(t)
	writeFile(t, filepath.Join(victim, ".aide.yaml"), "capabilities: [aws]\nsandbox: false\n")

	attacker := filepath.Join(filepath.Dir(victim), "download")
	admin := filepath.Join(attacker, "fakegit", "worktrees", "x")
	if err := os.MkdirAll(admin, 0o750); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(attacker, ".git"), "gitdir: ./fakegit/worktrees/x\n")
	writeFile(t, filepath.Join(admin, "commondir"), filepath.Join(victim, ".git")+"\n")
	writeFile(t, filepath.Join(admin, "HEAD"), "ref: refs/heads/master\n")
	writeFile(t, filepath.Join(admin, "gitdir"), filepath.Join(attacker, ".git")+"\n")

	d := DiscoverProject(attacker)
	if d.MainPath != "" || d.MainCheckoutRoot != "" {
		t.Fatalf("forged worktree inherited %q from %q", d.MainPath, d.MainCheckoutRoot)
	}
	if d.Err != nil {
		t.Errorf("forged worktree should inherit nothing, not stop: %v", d.Err)
	}
	if !strings.Contains(d.Warning, "not a registered worktree") {
		t.Errorf("Warning = %q, want a diagnostic", d.Warning)
	}
}

// symlinkOrSkip creates link -> target or skips the test.
func symlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
}

// A directory symlinked into another repository keeps the identity of
// where the file really lives: its relative paths are anchored to the
// checkout holding the file, never the repository the link sits in.
func TestLoad_RelativePathsAnchorAtResolvedCheckout(t *testing.T) {
	victim, _ := worktreeFixture(t)
	base := filepath.Dir(victim)
	if err := os.MkdirAll(filepath.Join(victim, "pkg"), 0o750); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(victim, "pkg", ".aide.yaml"), "sandbox:\n  writable_extra: [secrets]\n")
	attacker := filepath.Join(base, "evil")
	gitRun(t, base, "init", "-q", "evil")
	symlinkOrSkip(t, filepath.Join(victim, "pkg"), filepath.Join(attacker, "sub"))

	cfg, err := Load(writeGlobal(t), filepath.Join(attacker, "sub"))
	if err != nil {
		t.Fatal(err)
	}
	got := cfg.ProjectOverride.Sandbox.WritableExtra
	if want := filepath.Join(victim, "secrets"); len(got) != 1 || got[0] != want {
		t.Errorf("writable_extra = %v, want %s (anchored where the file lives, not in %s)", got, want, attacker)
	}
}

// ../ in a file reached through a symlinked checkout root stays
// relative to the real checkout.
func TestLoad_ParentRelativePathDoesNotEscapeIntoLinkingRepo(t *testing.T) {
	victim, _ := worktreeFixture(t)
	base := filepath.Dir(victim)
	writeFile(t, filepath.Join(victim, ".aide.yaml"), "sandbox:\n  readable_extra: [../x]\n")
	attacker := filepath.Join(base, "evil")
	gitRun(t, base, "init", "-q", "evil")
	symlinkOrSkip(t, victim, filepath.Join(attacker, "sub"))

	cfg, err := Load(writeGlobal(t), filepath.Join(attacker, "sub"))
	if err != nil {
		t.Fatal(err)
	}
	got := cfg.ProjectOverride.Sandbox.ReadableExtra
	if want := filepath.Join(base, "x"); len(got) != 1 || got[0] != want {
		t.Errorf("readable_extra = %v, want %s", got, want)
	}
}

// When the checkout holding a file cannot be determined, a layer with
// relative paths contributes nothing rather than guessing an anchor.
func TestLoad_UnanchorableRelativePathsDropTheLayer(t *testing.T) {
	dir := realTempDir(t)
	writeFile(t, filepath.Join(dir, ".aide.yaml"), "sandbox:\n  writable_extra: [build]\n")
	configDir := writeGlobal(t)
	t.Setenv("PATH", "")

	cfg, err := Load(configDir, dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ProjectOverride != nil || len(cfg.ProjectLayers) != 0 {
		t.Errorf("layer applied with an unknown anchor: %+v", cfg.ProjectOverride)
	}
	if len(cfg.ProjectConfigWarnings) != 1 || !strings.Contains(cfg.ProjectConfigWarnings[0], "relative") {
		t.Errorf("warnings = %v", cfg.ProjectConfigWarnings)
	}
}
