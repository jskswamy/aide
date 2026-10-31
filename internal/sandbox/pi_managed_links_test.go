package sandbox

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jskswamy/aide/internal/piinstall"
	"github.com/jskswamy/aide/internal/piinstall/testfixture"
	"github.com/jskswamy/aide/pkg/seatbelt/modules"
)

func TestPiManagedBootstrapSymlinks(t *testing.T) {
	f := testfixture.New(t)
	middle := filepath.Join(f.Home, "links", "pi")
	testfixture.Link(t, "../.pi/agent/bin/pi", middle)
	alias := filepath.Join(f.Home, "bin", "pi")
	testfixture.Link(t, "../links/pi", alias)
	data := filepath.Join(f.Home, ".local", "share")
	node := testfixture.Node(t, data)
	in, err := piinstall.Discover(alias, f.Home, []string{"HOME=" + f.Home}, nil)
	if err != nil {
		t.Fatal(err)
	}
	p := Policy{AgentModule: modules.PiAgentWithInstallation(in.Root, in.Readable)}
	args, err := piManagedSymlinkArgs(p, []string{in.Root, f.Launcher, node})
	if err != nil {
		t.Fatal(err)
	}
	for path, target := range map[string]string{alias: "../links/pi", middle: "../.pi/agent/bin/pi", filepath.Join(data, "pi-node", "current"): node} {
		found := false
		for i := 0; i+2 < len(args); i++ {
			if args[i] == "--symlink" && args[i+1] == target && args[i+2] == path {
				found = true
			}
		}
		if !found {
			t.Errorf("missing lexical link %s -> %s: %v", path, target, args)
		}
	}
	// A parent directory bind already exposes its symlinks; never recreate them
	// through a writable mount (which could modify the host's link).
	args, err = piManagedSymlinkArgs(p, []string{f.Home})
	if err != nil || len(args) != 0 {
		t.Fatalf("covered links recreated: %v %v", args, err)
	}
}

func TestPiManagedRelocatedInstallLink(t *testing.T) {
	f := testfixture.New(t)
	target := filepath.Join(f.Home, "shared-install")
	if err := os.Rename(f.Root, target); err != nil {
		t.Fatal(err)
	}
	testfixture.Link(t, target, f.Root)
	in, err := piinstall.Discover(f.Launcher, f.Home, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	p := Policy{AgentModule: modules.PiAgentWithInstallation(in.Root, in.Readable)}
	args, err := piManagedSymlinkArgs(p, []string{in.Root, f.Launcher})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for i := 0; i+2 < len(args); i++ {
		if args[i] == "--symlink" && args[i+1] == target && args[i+2] == f.Root {
			found = true
		}
	}
	if !found {
		t.Fatalf("relocated sibling root link lost: %v", args)
	}
}
