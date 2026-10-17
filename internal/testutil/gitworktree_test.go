package testutil_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/testutil"
)

func TestGitWorktree_CreatesLinkedWorktree(t *testing.T) {
	mainDir, wtDir := testutil.GitWorktree(t)

	// A linked worktree has a .git file, not a directory.
	if fi, err := os.Stat(filepath.Join(wtDir, ".git")); err != nil || fi.IsDir() {
		t.Errorf("%s/.git: want a file (linked worktree), got %v, err %v", wtDir, fi, err)
	}
	out, err := exec.Command("git", "-C", mainDir, "worktree", "list", "--porcelain").Output()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "worktree "+wtDir) {
		t.Errorf("git does not list %s as a worktree:\n%s", wtDir, out)
	}
}

func TestGit_RunsInDir(t *testing.T) {
	dir := testutil.CanonicalTempDir(t)
	testutil.Git(t, dir, "init", "-q")
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		t.Errorf("git init left no .git: %v", err)
	}
}
