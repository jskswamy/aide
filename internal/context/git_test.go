package context

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func skipIfNoGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found on PATH")
	}
}

// initGitRepo creates a temp git repo and returns its path.
func initGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cmd := exec.Command("git", "init", dir)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v\n%s", err, out)
	}
	return dir
}

func TestParseRemoteHost_SSHShorthand(t *testing.T) {
	got := ParseRemoteHost("git@github.com:jskswamy/aide.git")
	want := "github.com/jskswamy/aide"
	if got != want {
		t.Errorf("ParseRemoteHost SSH shorthand = %q, want %q", got, want)
	}
}

func TestParseRemoteHost_HTTPS(t *testing.T) {
	got := ParseRemoteHost("https://github.com/jskswamy/aide.git")
	want := "github.com/jskswamy/aide"
	if got != want {
		t.Errorf("ParseRemoteHost HTTPS = %q, want %q", got, want)
	}
}

func TestParseRemoteHost_HTTPSNoGit(t *testing.T) {
	got := ParseRemoteHost("https://github.com/jskswamy/aide")
	want := "github.com/jskswamy/aide"
	if got != want {
		t.Errorf("ParseRemoteHost HTTPS no .git = %q, want %q", got, want)
	}
}

func TestParseRemoteHost_SSHProtocol(t *testing.T) {
	got := ParseRemoteHost("ssh://git@github.com/jskswamy/aide")
	want := "github.com/jskswamy/aide"
	if got != want {
		t.Errorf("ParseRemoteHost SSH protocol = %q, want %q", got, want)
	}
}

func TestParseRemoteHost_Empty(t *testing.T) {
	got := ParseRemoteHost("")
	if got != "" {
		t.Errorf("ParseRemoteHost empty = %q, want empty", got)
	}
}

func TestParseRemoteHost_GitProtocol(t *testing.T) {
	got := ParseRemoteHost("git://github.com/org/repo.git")
	want := "github.com/org/repo"
	if got != want {
		t.Errorf("ParseRemoteHost git:// = %q, want %q", got, want)
	}
}

func TestDetectRemote_RealGitRepo(t *testing.T) {
	skipIfNoGit(t)
	dir := initGitRepo(t)

	// Add a remote
	cmd := exec.Command("git", "-C", dir, "remote", "add", "origin", "https://github.com/jskswamy/aide.git")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git remote add failed: %v\n%s", err, out)
	}

	got := DetectRemote(dir, "origin")
	want := "https://github.com/jskswamy/aide.git"
	if got != want {
		t.Errorf("DetectRemote = %q, want %q", got, want)
	}
}

func TestDetectRemote_NotAGitRepo(t *testing.T) {
	skipIfNoGit(t)
	dir := t.TempDir()
	got := DetectRemote(dir, "origin")
	if got != "" {
		t.Errorf("DetectRemote non-git = %q, want empty", got)
	}
}

func TestDetectRemote_NoRemote(t *testing.T) {
	skipIfNoGit(t)
	dir := initGitRepo(t)
	got := DetectRemote(dir, "origin")
	if got != "" {
		t.Errorf("DetectRemote no remote = %q, want empty", got)
	}
}

func TestDetectRemote_DefaultRemoteName(t *testing.T) {
	skipIfNoGit(t)
	dir := initGitRepo(t)

	cmd := exec.Command("git", "-C", dir, "remote", "add", "origin", "https://github.com/jskswamy/aide.git")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git remote add failed: %v\n%s", err, out)
	}

	// Empty remoteName should default to "origin"
	got := DetectRemote(dir, "")
	want := "https://github.com/jskswamy/aide.git"
	if got != want {
		t.Errorf("DetectRemote default remote = %q, want %q", got, want)
	}
}

func TestProjectRoot_GitRepo(t *testing.T) {
	skipIfNoGit(t)
	dir := initGitRepo(t)

	// Create a subdirectory
	subdir := filepath.Join(dir, "sub", "deep")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatal(err)
	}

	got := ProjectRoot(subdir)
	// Resolve symlinks for comparison (t.TempDir may use symlinked paths)
	wantResolved, _ := filepath.EvalSymlinks(dir)
	gotResolved, _ := filepath.EvalSymlinks(got)
	if gotResolved != wantResolved {
		t.Errorf("ProjectRoot = %q, want %q", gotResolved, wantResolved)
	}
}

func TestProjectRoot_NotGitRepo(t *testing.T) {
	skipIfNoGit(t)
	dir := t.TempDir()
	got := ProjectRoot(dir)
	// Should fall back to the dir itself
	wantResolved, _ := filepath.EvalSymlinks(dir)
	gotResolved, _ := filepath.EvalSymlinks(got)
	if gotResolved != wantResolved {
		t.Errorf("ProjectRoot fallback = %q, want %q", gotResolved, wantResolved)
	}
}
