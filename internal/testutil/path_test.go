package testutil_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/testutil"
)

func TestCanonicalTempDir_ResolvesMacOSPrivateVarPrefix(t *testing.T) {
	// On macOS, t.TempDir returns paths under /var/folders/... which
	// is itself a symlink to /private/var/folders/.... Tests that
	// compare against the kernel-resolved path (the form seatbelt
	// rule matching uses) need the canonical form. CanonicalTempDir
	// guarantees that.
	got := testutil.CanonicalTempDir(t)
	resolved, err := filepath.EvalSymlinks(got)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", got, err)
	}
	if got != resolved {
		t.Errorf("CanonicalTempDir returned non-canonical path %q; canonical form is %q", got, resolved)
	}
}

func TestCanonicalTempDir_Isolated(t *testing.T) {
	// Two consecutive calls within the same test must return
	// different directories so callers can build independent
	// fixtures.
	a := testutil.CanonicalTempDir(t)
	b := testutil.CanonicalTempDir(t)
	if a == b {
		t.Errorf("CanonicalTempDir returned the same dir twice: %q", a)
	}
}

func TestCanonicalTempDir_Writable(t *testing.T) {
	// The returned dir must accept writes — same contract as
	// t.TempDir. A test that thinks it has a writable scratch dir
	// must actually get one.
	dir := testutil.CanonicalTempDir(t)
	target := filepath.Join(dir, "probe")
	if err := os.WriteFile(target, []byte("x"), 0o600); err != nil {
		t.Fatalf("write probe: %v", err)
	}
	if !strings.HasPrefix(target, dir+string(filepath.Separator)) {
		t.Errorf("probe path %q not under returned dir %q", target, dir)
	}
}
