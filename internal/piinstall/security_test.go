package piinstall_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/piinstall"
	"github.com/jskswamy/aide/internal/piinstall/testfixture"
)

func TestDiscoverLargeNonManagedUnchanged(t *testing.T) {
	home := t.TempDir()
	binary := filepath.Join(home, "bin", "pi")
	testfixture.Write(t, binary, strings.Repeat("x", 100<<10), 0755)
	result, err := piinstall.Discover(binary, home, nil, nil)
	if err != nil || result != nil {
		t.Fatalf("unrelated large binary changed behavior: %v %v", result, err)
	}
}

func TestDiscoverRejectsPrivateFilesystemRoot(t *testing.T) {
	f := testfixture.New(t)
	result, err := piinstall.Discover(f.Launcher, f.Home, nil, []string{"/"})
	if err == nil || result != nil {
		t.Fatalf("private filesystem root ignored: %v %v", result, err)
	}
}

func TestDiscoverRejectsDefaultPrivateSubtree(t *testing.T) {
	f := testfixture.New(t)
	private := filepath.Join(f.Agent, "private-state")
	if err := os.Rename(f.Root, private); err != nil {
		t.Fatal(err)
	}
	testfixture.Link(t, private, f.Root)
	result, err := piinstall.Discover(f.Launcher, f.Home, nil, nil)
	if err == nil || result != nil {
		t.Fatalf("private default-state subtree accepted: %v %v", result, err)
	}
}
