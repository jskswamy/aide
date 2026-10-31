package piinstall_test

import (
	"path/filepath"
	"testing"

	"github.com/jskswamy/aide/internal/piinstall"
	"github.com/jskswamy/aide/internal/piinstall/testfixture"
)

func TestManagedExecutableParentsProtectAdjacentState(t *testing.T) {
	f := testfixture.New(t)
	alias := filepath.Join(f.Home, "bin", "pi")
	testfixture.Link(t, f.Launcher, alias)
	private := filepath.Join(f.Home, "bin", "personal")
	in, err := piinstall.Discover(alias, f.Home, nil, []string{private})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := in.ExecutableParents(alias, f.Home, []string{private}); err == nil {
		t.Fatal("Landlock bootstrap parent exposes adjacent private profile")
	}
	if parents, err := in.ExecutableParents(f.Launcher, f.Home, nil); err != nil || len(parents) == 0 {
		t.Fatalf("dedicated launcher bin rejected: %v %v", parents, err)
	}
}
