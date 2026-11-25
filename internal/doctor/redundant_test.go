package doctor

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/jskswamy/aide/internal/config"
)

func TestRedundantAllows(t *testing.T) {
	home := t.TempDir()
	for _, d := range []string{"src/a/b", "src/a/secret", "real/x", "data"} {
		if err := os.MkdirAll(filepath.Join(home, d), 0o750); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(home, "real"), filepath.Join(home, "link")); err != nil {
		t.Fatal(err)
	}
	// A guarded dotfile that links to a plain name is still guarded.
	if err := os.WriteFile(filepath.Join(home, "src/a/settings"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(home, "src/a/settings"), filepath.Join(home, "src/a/.envlink")); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir() // a link target outside home is not widened
	if err := os.MkdirAll(filepath.Join(outside, "sub"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(home, "ext")); err != nil {
		t.Fatal(err)
	}
	sp := &config.SandboxPolicy{
		ReadableExtra: []string{
			"~/src/a", "~/src/a/b", // b is covered
			"~/src/a/secret",     // a deny sits under the parent: kept
			"~/link", "~/real/x", // covered through the symlink
			"~/data", "~/data", // the same text twice: removing one removes both, so kept
			"~/src/a/.env", "~/src/a/b/.config/gh", // guards need these listed exactly: kept
			"~/src/a/.envlink",                     // a dotfile link to a plain name: kept
			"~/ext", filepath.Join(outside, "sub"), // the sandbox grants ~/ext only literally: kept
			"{{ .project_root }}/x", "rel/path", // skipped
		},
		WritableExtra: []string{"~/src/a/b/out"}, // a different list: kept
		DeniedExtra:   []string{"~/src/a/secret/keys"},
	}
	got := RedundantAllows(sp, home, Scope{Profile: "work", Global: true})
	var subjects []string
	for _, f := range got {
		subjects = append(subjects, f.Subject)
		if f.ID != "sandbox.redundant-allow" || f.Severity != SeverityTip || f.WidensAccess {
			t.Errorf("finding = %+v", f)
		}
	}
	want := []string{"~/src/a/b", "~/real/x"}
	if !slices.Equal(subjects, want) {
		t.Errorf("redundant = %v, want %v", subjects, want)
	}
	if !slices.Equal(got[0].Fix.Argv, []string{"aide", "sandbox", "allow", "--remove", "~/src/a/b", "--global", "--profile", "work"}) {
		t.Errorf("argv = %v", got[0].Fix.Argv)
	}
}

func TestRedundantAllows_FixPerScope(t *testing.T) {
	home := t.TempDir()
	sp := &config.SandboxPolicy{WritableExtra: []string{"~/w", "~/w/x"}}
	for _, tc := range []struct {
		scope Scope
		argv  []string
	}{
		{Scope{Profile: "work"}, []string{"aide", "sandbox", "allow", "--remove", "--write", "~/w/x"}},
		{Scope{Profile: "work", Named: "shared"}, []string{"aide", "sandbox", "edit", "shared", "--remove-writable", "~/w/x"}},
		{Scope{Profile: "work", Inherited: true}, nil},
	} {
		got := RedundantAllows(sp, home, tc.scope)
		if len(got) != 1 {
			t.Fatalf("%+v: got %v", tc.scope, got)
		}
		if !slices.Equal(got[0].Fix.Argv, tc.argv) {
			t.Errorf("%+v: argv = %v, want %v", tc.scope, got[0].Fix.Argv, tc.argv)
		}
		if tc.argv == nil && len(got[0].Fix.Steps) == 0 {
			t.Errorf("%+v: want written steps", tc.scope)
		}
	}
}
