package modules

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// fakeHome returns a temp dir that mimics $HOME for tests in this file. It
// must be a real path because expandConfigDirWritable uses EvalSymlinks and
// the safety filter checks fsutil.IsUnderDir.
func fakeHome(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

func TestExpandConfigDirWritable_DirAlwaysIncluded(t *testing.T) {
	home := fakeHome(t)
	dir := filepath.Join(home, ".cursor")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("setup: %v", err)
	}

	got := expandConfigDirWritable(home, dir)
	if !slices.Contains(got, dir) {
		t.Errorf("expected %q in result; got %v", dir, got)
	}
}

func TestExpandConfigDirWritable_MissingDirReturnsOnlyDir(t *testing.T) {
	home := fakeHome(t)
	missing := filepath.Join(home, ".cursor")

	got := expandConfigDirWritable(home, missing)
	if len(got) != 1 || got[0] != missing {
		t.Errorf("expected exactly [%q] for missing dir; got %v", missing, got)
	}
}

func TestExpandConfigDirWritable_WholeDirSymlinkIncludesTarget(t *testing.T) {
	home := fakeHome(t)
	target := filepath.Join(home, "dotfiles", "cursor")
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatalf("setup target: %v", err)
	}
	dir := filepath.Join(home, ".cursor")
	if err := os.Symlink(target, dir); err != nil {
		t.Fatalf("setup symlink: %v", err)
	}

	got := expandConfigDirWritable(home, dir)

	if !slices.Contains(got, dir) {
		t.Errorf("expected literal dir %q in result; got %v", dir, got)
	}
	if !slices.Contains(got, target) {
		t.Errorf("expected resolved target %q in result; got %v", target, got)
	}
}

func TestExpandConfigDirWritable_Depth1DirSymlinkTightScope(t *testing.T) {
	home := fakeHome(t)
	dir := filepath.Join(home, ".cursor")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("setup dir: %v", err)
	}
	skillsTarget := filepath.Join(home, "dotfiles", "cursor-skills")
	if err := os.MkdirAll(skillsTarget, 0o700); err != nil {
		t.Fatalf("setup target: %v", err)
	}
	if err := os.Symlink(skillsTarget, filepath.Join(dir, "skills")); err != nil {
		t.Fatalf("setup symlink: %v", err)
	}

	got := expandConfigDirWritable(home, dir)

	if !slices.Contains(got, skillsTarget) {
		t.Errorf("symlink-to-dir target %q must be in result for Landlock to allow writes; got %v",
			skillsTarget, got)
	}
	// Tight scope: parent of target (~/dotfiles/) must NOT be granted —
	// that would let the agent reach sibling dotfiles it has no business with.
	parent := filepath.Dir(skillsTarget)
	if slices.Contains(got, parent) {
		t.Errorf("dir-symlink expansion should grant target itself, not its parent %q; got %v",
			parent, got)
	}
}

func TestExpandConfigDirWritable_Depth1FileSymlinkParentScope(t *testing.T) {
	home := fakeHome(t)
	dir := filepath.Join(home, ".cursor")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("setup dir: %v", err)
	}
	// Symlink to a file (e.g. user has ~/.cursor/settings.json -> ~/dotfiles/cursor-settings.json)
	dotfilesDir := filepath.Join(home, "dotfiles")
	if err := os.MkdirAll(dotfilesDir, 0o700); err != nil {
		t.Fatalf("setup dotfiles: %v", err)
	}
	targetFile := filepath.Join(dotfilesDir, "cursor-settings.json")
	if err := os.WriteFile(targetFile, []byte("{}"), 0o600); err != nil {
		t.Fatalf("setup target file: %v", err)
	}
	if err := os.Symlink(targetFile, filepath.Join(dir, "settings.json")); err != nil {
		t.Fatalf("setup symlink: %v", err)
	}

	got := expandConfigDirWritable(home, dir)

	// File symlink: grant the PARENT so atomic-write tmp+rename siblings work.
	if !slices.Contains(got, dotfilesDir) {
		t.Errorf("symlink-to-file should grant target's parent %q for atomic-rename support; got %v",
			dotfilesDir, got)
	}
	if slices.Contains(got, targetFile) {
		t.Errorf("symlink-to-file should grant parent, not file literal %q; got %v",
			targetFile, got)
	}
}

func TestExpandConfigDirWritable_UnsafeTargetSkipped(t *testing.T) {
	home := fakeHome(t)
	dir := filepath.Join(home, ".cursor")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("setup: %v", err)
	}
	// Symlink to ~/.ssh (sensitive) — must NOT be granted.
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatalf("setup ssh: %v", err)
	}
	if err := os.Symlink(sshDir, filepath.Join(dir, "keys")); err != nil {
		t.Fatalf("setup symlink: %v", err)
	}

	got := expandConfigDirWritable(home, dir)

	if slices.Contains(got, sshDir) {
		t.Errorf("expansion must not widen to ~/.ssh; got %v", got)
	}
}

func TestExpandConfigDirWritable_TargetOutsideHomeSkipped(t *testing.T) {
	home := fakeHome(t)
	dir := filepath.Join(home, ".cursor")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("setup: %v", err)
	}
	// Symlink to /tmp/<something> — outside home, must NOT be granted.
	outside := t.TempDir() // a different tempdir, not under `home`
	if err := os.Symlink(outside, filepath.Join(dir, "stuff")); err != nil {
		t.Fatalf("setup symlink: %v", err)
	}

	got := expandConfigDirWritable(home, dir)

	if slices.Contains(got, outside) {
		t.Errorf("expansion must not widen to paths outside $HOME; got %v", got)
	}
}

func TestExpandConfigDirWritable_BrokenSymlinkSkipped(t *testing.T) {
	home := fakeHome(t)
	dir := filepath.Join(home, ".cursor")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("setup: %v", err)
	}
	missingTarget := filepath.Join(home, "does-not-exist")
	if err := os.Symlink(missingTarget, filepath.Join(dir, "broken")); err != nil {
		t.Fatalf("setup symlink: %v", err)
	}

	got := expandConfigDirWritable(home, dir)

	for _, p := range got {
		if p == missingTarget || p == filepath.Dir(missingTarget) {
			t.Errorf("broken symlink should not contribute to result; got %v", got)
		}
	}
}

// TestExpandConfigDirWritable_ChainedSymlinkTargets is the production bug:
// ~/.cursor/skills -> ~/.claude/skills, and ~/.claude/skills/dd-apm is itself
// a symlink to ~/.agents/skills/dd-apm. Both intermediate and final targets
// must end up in the writable set, or Landlock denies access at the resolved
// inode (the user's reported symptom).
func TestExpandConfigDirWritable_ChainedSymlinkTargets(t *testing.T) {
	home := fakeHome(t)

	// Layer 3: real skill directory the user actually maintains
	realSkill := filepath.Join(home, ".agents", "skills", "dd-apm")
	if err := os.MkdirAll(realSkill, 0o700); err != nil {
		t.Fatalf("setup real skill: %v", err)
	}

	// Layer 2: shared skills dir the user manages
	claudeSkills := filepath.Join(home, ".claude", "skills")
	if err := os.MkdirAll(claudeSkills, 0o700); err != nil {
		t.Fatalf("setup claude skills: %v", err)
	}
	if err := os.Symlink(realSkill, filepath.Join(claudeSkills, "dd-apm")); err != nil {
		t.Fatalf("setup dd-apm symlink: %v", err)
	}

	// Layer 1: per-agent dir symlinks `skills/` to the shared dir
	cursorDir := filepath.Join(home, ".cursor")
	if err := os.MkdirAll(cursorDir, 0o700); err != nil {
		t.Fatalf("setup cursor dir: %v", err)
	}
	if err := os.Symlink(claudeSkills, filepath.Join(cursorDir, "skills")); err != nil {
		t.Fatalf("setup skills symlink: %v", err)
	}

	got := expandConfigDirWritable(home, cursorDir)

	// The depth-1 hop (cursor/skills → claude/skills) must contribute its
	// target. Already worked before this fix.
	if !slices.Contains(got, claudeSkills) {
		t.Errorf("missing intermediate target %q; got %v", claudeSkills, got)
	}
	// The depth-2 hop (claude/skills/dd-apm → agents/skills/dd-apm) must
	// also contribute — the original bug.
	if !slices.Contains(got, realSkill) {
		t.Errorf("missing nested-symlink target %q; depth-2 expansion failed; got %v", realSkill, got)
	}
}

// TestExpandConfigDirWritable_DoesNotLoop pins the visited-set behaviour
// against the pathological self-referencing symlink pattern. The walk must
// terminate (it does, because EvalSymlinks itself rejects loops; the
// visited set is belt-and-braces for the case where EvalSymlinks resolved
// to a path we've already seen via a different chain).
func TestExpandConfigDirWritable_DoesNotLoop(t *testing.T) {
	home := fakeHome(t)
	dir := filepath.Join(home, ".cursor")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("setup: %v", err)
	}
	// a/ and b/ are real dirs; a/sym -> b/, b/sym -> a/. Walk should
	// visit each once and return.
	a := filepath.Join(home, "a")
	b := filepath.Join(home, "b")
	if err := os.MkdirAll(a, 0o700); err != nil {
		t.Fatalf("setup a: %v", err)
	}
	if err := os.MkdirAll(b, 0o700); err != nil {
		t.Fatalf("setup b: %v", err)
	}
	if err := os.Symlink(a, filepath.Join(b, "sym")); err != nil {
		t.Fatalf("setup b->a symlink: %v", err)
	}
	if err := os.Symlink(b, filepath.Join(a, "sym")); err != nil {
		t.Fatalf("setup a->b symlink: %v", err)
	}
	if err := os.Symlink(a, filepath.Join(dir, "start")); err != nil {
		t.Fatalf("setup cursor symlink: %v", err)
	}

	done := make(chan []string, 1)
	go func() { done <- expandConfigDirWritable(home, dir) }()

	select {
	case got := <-done:
		// Both a and b should appear; cursor itself is always included.
		if !slices.Contains(got, a) || !slices.Contains(got, b) {
			t.Errorf("expected both %q and %q in result; got %v", a, b, got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expandConfigDirWritable did not terminate within 2s — visited-set is not bounding the walk")
	}
}

func TestExpandConfigDirWritable_NonSymlinkEntriesIgnored(t *testing.T) {
	home := fakeHome(t)
	dir := filepath.Join(home, ".cursor")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("setup: %v", err)
	}
	// Regular file and regular dir — neither should contribute.
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte("{}"), 0o600); err != nil {
		t.Fatalf("setup file: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "real-skills"), 0o700); err != nil {
		t.Fatalf("setup subdir: %v", err)
	}

	got := expandConfigDirWritable(home, dir)

	if len(got) != 1 || got[0] != dir {
		t.Errorf("non-symlink entries should not contribute; want [%q], got %v", dir, got)
	}
}
