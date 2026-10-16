package testutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Git runs git in dir with an isolated identity and configuration,
// failing the test on error.
func Git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// GitWorktree creates <tmp>/main, a repository with one commit, and
// <tmp>/wt, a real linked worktree of it (git worktree add). Paths are
// symlink-resolved so they compare equal to what git reports. It skips
// the test when git is unavailable.
func GitWorktree(t *testing.T) (mainDir, wtDir string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	mainDir = filepath.Join(base, "main")
	wtDir = filepath.Join(base, "wt")
	Git(t, base, "init", "-q", "main")
	Git(t, mainDir, "commit", "-q", "--allow-empty", "-m", "init")
	Git(t, mainDir, "worktree", "add", "-q", "-b", "wt", wtDir)
	return mainDir, wtDir
}
