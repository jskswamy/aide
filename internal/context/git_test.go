package context

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

func TestParseRemoteHost_Normalizes(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"scp", "git@github.com:org/repo.git", "github.com/org/repo"},
		{"scp no user", "github.com:org/repo.git", "github.com/org/repo"},
		{"ssh scheme", "ssh://git@github.com/org/repo.git", "github.com/org/repo"},
		{"https", "https://github.com/org/repo.git", "github.com/org/repo"},
		{"git scheme", "git://github.com/org/repo.git", "github.com/org/repo"},
		{"canonical", "github.com/org/repo", "github.com/org/repo"},
		{"uppercase host https", "https://GitHub.COM/Org/Repo.git", "github.com/Org/Repo"},
		{"uppercase host scp", "git@GitHub.com:Org/Repo.git", "github.com/Org/Repo"},
		{"uppercase host canonical", "GitHub.com/Org/Repo", "github.com/Org/Repo"},
		{"trailing slash https", "https://github.com/org/repo/", "github.com/org/repo"},
		{"trailing slash after .git", "https://github.com/org/repo.git/", "github.com/org/repo"},
		{"trailing slash scp", "git@github.com:org/repo/", "github.com/org/repo"},
		{"trailing slash canonical", "github.com/org/repo/", "github.com/org/repo"},
		{"ssh port", "ssh://git@git.example.com:2222/org/repo.git", "git.example.com:2222/org/repo"},
		{"https port", "https://git.example.com:8443/org/repo", "git.example.com:8443/org/repo"},
		{"https port uppercase", "https://Git.Example.com:8443/org/repo", "git.example.com:8443/org/repo"},
		{"canonical with port", "git.example.com:2222/org/repo", "git.example.com:2222/org/repo"},
		{"ipv6 with port", "ssh://git@[::1]:2222/org/repo.git", "[::1]:2222/org/repo"},
		{"surrounding whitespace", "  https://github.com/org/repo.git\n", "github.com/org/repo"},
		{"local path", "/srv/git/repo.git", ""},
		{"file url", "file:///srv/git/repo.git", ""},
		{"empty", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ParseRemoteHost(tc.in); got != tc.want {
				t.Errorf("ParseRemoteHost(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestParseRemoteHost_NeverEmitsUserinfo(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"https://user:s3cret@github.com/org/repo.git", "github.com/org/repo"},
		{"https://ghp_TOKEN@github.com/org/repo.git", "github.com/org/repo"},
		{"https://oauth2:s3cret@gitlab.example.com:8443/org/repo.git", "gitlab.example.com:8443/org/repo"},
		{"ssh://user:s3cret@github.com/org/repo.git", "github.com/org/repo"},
		{"user:s3cret@github.com:org/repo.git", "github.com/org/repo"},
		// Unparseable URLs must not be echoed back verbatim.
		{"https://user:s3cret@github.com:badport/org/repo.git", ""},
		{"https://user:s3cret@%zz/org/repo", ""},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got := ParseRemoteHost(tc.in)
			if got != tc.want {
				t.Errorf("ParseRemoteHost(%q) = %q, want %q", tc.in, got, tc.want)
			}
			for _, secret := range []string{"s3cret", "ghp_TOKEN", "@"} {
				if strings.Contains(got, secret) {
					t.Errorf("ParseRemoteHost(%q) = %q leaks %q", tc.in, got, secret)
				}
			}
		})
	}
}

func TestParseRemoteHost_Idempotent(t *testing.T) {
	inputs := []string{
		"git@GitHub.com:Org/Repo.git",
		"ssh://git@git.example.com:2222/org/repo.git/",
		"https://user:pw@Example.com:8443/a/b/c.git",
		"ssh://git@[::1]:2222/org/repo.git",
	}
	for _, in := range inputs {
		once := ParseRemoteHost(in)
		if twice := ParseRemoteHost(once); twice != once {
			t.Errorf("ParseRemoteHost not idempotent for %q: %q then %q", in, once, twice)
		}
	}
}

func addRemote(t *testing.T, dir, name, url string) {
	t.Helper()
	cmd := exec.Command("git", "-C", dir, "remote", "add", name, url)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git remote add %s failed: %v\n%s", name, err, out)
	}
}

func TestCurrentRemote_NormalizesOriginWithoutCredentials(t *testing.T) {
	skipIfNoGit(t)
	dir := initGitRepo(t)
	addRemote(t, dir, "origin", "https://user:s3cret@GitHub.com/org/repo.git/")
	addRemote(t, dir, "upstream", "https://github.com/upstream/repo.git")

	if got, want := CurrentRemote(dir), "github.com/org/repo"; got != want {
		t.Errorf("CurrentRemote = %q, want %q", got, want)
	}
}

func TestCurrentRemote_NoOrigin(t *testing.T) {
	skipIfNoGit(t)
	dir := initGitRepo(t)
	addRemote(t, dir, "upstream", "https://github.com/upstream/repo.git")

	if got := CurrentRemote(dir); got != "" {
		t.Errorf("CurrentRemote without origin = %q, want empty", got)
	}
	if got := CurrentRemote(t.TempDir()); got != "" {
		t.Errorf("CurrentRemote outside a repo = %q, want empty", got)
	}
}

func TestGitRemotes_LooksUpNamedRemotes(t *testing.T) {
	skipIfNoGit(t)
	dir := initGitRepo(t)
	addRemote(t, dir, "origin", "git@github.com:me/repo.git")
	addRemote(t, dir, "upstream", "https://github.com/upstream/repo.git")

	remotes := GitRemotes(dir)
	cases := map[string]string{
		"":         "git@github.com:me/repo.git",
		"origin":   "git@github.com:me/repo.git",
		"upstream": "https://github.com/upstream/repo.git",
		"missing":  "",
	}
	for name, want := range cases {
		if got := remotes(name); got != want {
			t.Errorf("GitRemotes(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestParseRemoteHost_DropsSchemeDefaultPorts(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"ssh://git@github.com:22/org/repo.git", "github.com/org/repo"},
		{"https://github.com:443/org/repo.git", "github.com/org/repo"},
		{"http://git.example.com:80/org/repo", "git.example.com/org/repo"},
		{"git://github.com:9418/org/repo.git", "github.com/org/repo"},
		{"git+ssh://git@github.com:22/org/repo.git", "github.com/org/repo"},
		{"ssh+git://git@github.com:22/org/repo.git", "github.com/org/repo"},
		{"HTTPS://GitHub.com:443/org/repo", "github.com/org/repo"},
		// A port that is only another scheme's default is kept.
		{"https://git.example.com:22/org/repo", "git.example.com:22/org/repo"},
		{"ssh://git@git.example.com:443/org/repo", "git.example.com:443/org/repo"},
		{"ssh://git@git.example.com:2222/org/repo", "git.example.com:2222/org/repo"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			if got := ParseRemoteHost(tc.in); got != tc.want {
				t.Errorf("ParseRemoteHost(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestParseRemoteHost_SCPPasswordWithSlash(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"user:pa/ss@github.com:org/repo.git", "github.com/org/repo"},
		{"user:p@ss/w:rd@github.com:org/repo.git", "github.com/org/repo"},
		// '@' in the path is not userinfo.
		{"git@github.com:org/repo@v1", "github.com/org/repo@v1"},
		// No host can be found: emit nothing rather than userinfo.
		{"user:s3cret@github.com", ""},
		{"user:pa/ss@github.com", ""},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got := ParseRemoteHost(tc.in)
			if got != tc.want {
				t.Errorf("ParseRemoteHost(%q) = %q, want %q", tc.in, got, tc.want)
			}
			for _, secret := range []string{"pa/ss", "ss", "s3cret", "user"} {
				if got != "" && strings.Contains(got, secret) {
					t.Errorf("ParseRemoteHost(%q) = %q leaks %q", tc.in, got, secret)
				}
			}
		})
	}
}

func TestStripRemoteUserinfo(t *testing.T) {
	cases := map[string]string{
		"https://ghp_TOKEN@github.com/org/*":  "https://github.com/org/*",
		"https://user:pa/ss@github.com/org/*": "https://github.com/org/*",
		"git@github.com:org/*":                "github.com:org/*",
		"user:p@ss@github.com:org/*":          "github.com:org/*",
		"github.com/org/*":                    "github.com/org/*",
		"github.com/org/repo@v*":              "github.com/org/repo@v*",
		"*.internal.corp/*":                   "*.internal.corp/*",
		"https://user:s3cret@github.com":      "https://github.com",
		"user:pa/ss@github.com":               "github.com",
	}
	for in, want := range cases {
		if got := stripRemoteUserinfo(in); got != want {
			t.Errorf("stripRemoteUserinfo(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseRemoteHost_URLUserinfoFailsClosed(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		// An unescaped '/' in the password makes url.Parse read
		// "user:12" as host:port; refuse instead of emitting "12/ss@".
		{"https://user:12/ss@host/x", ""},
		{"https://a@user:12/ss@host/x", ""},
		{"ssh://git:34/pw@github.com/org/repo.git", ""},
		// Well-formed userinfo is still stripped.
		{"https://tok@github.com/org/repo.git", "github.com/org/repo"},
		{"https://user:p%2Fw@github.com/org/repo.git", "github.com/org/repo"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got := ParseRemoteHost(tc.in)
			if got != tc.want {
				t.Errorf("ParseRemoteHost(%q) = %q, want %q", tc.in, got, tc.want)
			}
			if strings.Contains(got, "@") {
				t.Errorf("ParseRemoteHost(%q) = %q contains userinfo", tc.in, got)
			}
		})
	}
}

func TestParseRemoteHost_SCPPathAtBeforeSlash(t *testing.T) {
	// The '@' in "a@b/c" is part of the path, not userinfo.
	if got, want := ParseRemoteHost("git@h.example.com:org/a@b/c"), "h.example.com/org/a@b/c"; got != want {
		t.Errorf("ParseRemoteHost = %q, want %q", got, want)
	}
}
