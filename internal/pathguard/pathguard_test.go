package pathguard

import (
	"os"
	"path/filepath"
	"testing"
)

func noEnv(string) string { return "" }

func TestSafeSeatbeltPath(t *testing.T) {
	valid := []string{
		"/Users/alice/.local/state/terminal-browser-58a67c5b",
		"/run/user/1000/terminal-browser-dev-abcdef01",
		"/tmp/my dir/sub-1",
	}
	for _, p := range valid {
		if !SafeSeatbeltPath(p) {
			t.Errorf("SafeSeatbeltPath(%q) = false, want true", p)
		}
	}

	invalid := []string{
		"",
		`/Users/alice/foo"bar`,
		`/Users/alice/foo\bar`,
		`/Users/alice/foo(bar)`,
		"/Users/alice/foo\nbar",
		"/Users/alice/foo\rbar",
		"/Users/alice/foo\x00bar",
		"/Users/alice/foo\x1fbar",
		"/Users/alice/foo\x7fbar",
	}
	for _, p := range invalid {
		if SafeSeatbeltPath(p) {
			t.Errorf("SafeSeatbeltPath(%q) = true, want false", p)
		}
	}
}

func TestChecker_GrantDir(t *testing.T) {
	home := t.TempDir()
	c := NewChecker(nil, home, noEnv)

	okDir := filepath.Join(home, ".local", "state", "terminal-browser-58a67c5b")
	if err := os.MkdirAll(okDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := c.GrantDir(okDir, true); len(got) == 0 {
		t.Errorf("GrantDir(%q, true) = nil, want non-empty", okDir)
	}

	// Regular file must be rejected by GrantDir.
	regFile := filepath.Join(home, ".local", "state", "not-a-dir")
	if err := os.WriteFile(regFile, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := c.GrantDir(regFile, true); len(got) != 0 {
		t.Errorf("GrantDir(file) = %v, want nil", got)
	}

	// $HOME itself must be rejected.
	if got := c.GrantDir(home, true); len(got) != 0 {
		t.Errorf("GrantDir(home) = %v, want nil", got)
	}

	// Sensitive dir (~/.ssh) must be rejected.
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if got := c.GrantDir(sshDir, true); len(got) != 0 {
		t.Errorf("GrantDir(~/.ssh) = %v, want nil", got)
	}

	// Symlink into ~/.ssh must be rejected.
	evilLink := filepath.Join(home, ".local", "state", "evil-link")
	if err := os.Symlink(sshDir, evilLink); err != nil {
		t.Fatal(err)
	}
	if got := c.GrantDir(evilLink, true); len(got) != 0 {
		t.Errorf("GrantDir(symlink -> ~/.ssh) = %v, want nil", got)
	}

	// System root (/etc) must be rejected when requireWritableRoot is true.
	if got := c.GrantDir("/etc", true); len(got) != 0 {
		t.Errorf("GrantDir(/etc, true) = %v, want nil", got)
	}

	// Writable root itself (os.TempDir()) must be rejected (strict containment).
	if got := c.GrantDir(os.TempDir(), true); len(got) != 0 {
		t.Errorf("GrantDir(os.TempDir(), true) = %v, want nil", got)
	}

	// Subdirectory of os.TempDir() is allowed when not a system root.
	tmpSub := filepath.Join(t.TempDir(), "tb-interop")
	if err := os.MkdirAll(tmpSub, 0o755); err != nil {
		t.Fatal(err)
	}
	// On macOS t.TempDir() is under /var/folders/...; on Linux /tmp is excluded
	// by systemRoots. Only assert non-empty when tmpSub is outside /tmp.
	if resolved, err := filepath.EvalSymlinks(tmpSub); err == nil {
		if filepath.Dir(resolved) != "/tmp" && filepath.Dir(resolved) != "/private/tmp" {
			if c.allowedWritableRoot(resolved) {
				if got := c.GrantDir(tmpSub, true); len(got) == 0 {
					t.Errorf("GrantDir(%q, true) = nil, want non-empty", tmpSub)
				}
			}
		}
	}
}
