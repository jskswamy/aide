package fsutil_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/jskswamy/aide/internal/fsutil"
	"github.com/jskswamy/aide/internal/testutil"
)

// TestResolveWidening pins the contract the seatbelt filesystem guard and
// `aide cap show` need: given a (pre-tilde-expanded) capability path and
// $HOME, report whether the path resolves elsewhere via EvalSymlinks and
// whether that resolved target lives strictly inside the safety floor
// (under-$HOME). Each caller picks its own response — the guard drops
// resolved-outside-$HOME targets to prevent silent broadening; cap show
// annotates them with a warning so the user can audit.
func TestResolveWidening(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks require elevated privileges on windows")
	}
	home := testutil.CanonicalTempDir(t)

	t.Run("non-symlink path: changed=false", func(t *testing.T) {
		regular := filepath.Join(home, "plain.txt")
		if err := os.WriteFile(regular, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		resolved, changed, underHome := fsutil.ResolveWidening(regular, home)
		if changed {
			t.Errorf("changed = true for non-symlink %q; resolved = %q", regular, resolved)
		}
		if !underHome {
			t.Errorf("underHome = false for path %q under %q", regular, home)
		}
	})

	t.Run("symlink under home: changed=true, underHome=true", func(t *testing.T) {
		target := filepath.Join(home, "target.txt")
		link := filepath.Join(home, "link.txt")
		if err := os.WriteFile(target, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		resolved, changed, underHome := fsutil.ResolveWidening(link, home)
		if !changed {
			t.Errorf("changed = false for symlink %q", link)
		}
		if resolved != target {
			t.Errorf("resolved = %q, want %q", resolved, target)
		}
		if !underHome {
			t.Errorf("underHome = false for resolved %q under %q", resolved, home)
		}
	})

	t.Run("symlink whose target escapes home: changed=true, underHome=false", func(t *testing.T) {
		outside := testutil.CanonicalTempDir(t) // a different tmpdir, not under home
		escapeTarget := filepath.Join(outside, "secret.txt")
		if err := os.WriteFile(escapeTarget, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(home, "escape-link")
		if err := os.Symlink(escapeTarget, link); err != nil {
			t.Fatal(err)
		}
		resolved, changed, underHome := fsutil.ResolveWidening(link, home)
		if !changed {
			t.Errorf("changed = false for symlink %q -> outside-home %q", link, escapeTarget)
		}
		if underHome {
			t.Errorf("underHome = true for resolved %q outside home %q", resolved, home)
		}
	})

	t.Run("missing path: changed=false (graceful fallback)", func(t *testing.T) {
		missing := filepath.Join(home, "does-not-exist.yaml")
		resolved, changed, _ := fsutil.ResolveWidening(missing, home)
		if changed {
			t.Errorf("changed = true for non-existent path %q (resolved = %q)", missing, resolved)
		}
	})

	t.Run("empty home: changed reported but underHome always false", func(t *testing.T) {
		// Edge case for callers that legitimately pass home="" (e.g. a
		// stripped-down launcher context). Resolution still happens; the
		// safety classification just degenerates to "outside everything".
		target := filepath.Join(home, "h-target")
		link := filepath.Join(home, "h-link")
		if err := os.WriteFile(target, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		_, changed, underHome := fsutil.ResolveWidening(link, "")
		if !changed {
			t.Error("changed = false; expected symlink to be detected even with empty home")
		}
		if underHome {
			t.Error("underHome = true with empty home; classification must default to false")
		}
	})
}
