package sandbox

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/jskswamy/aide/internal/piinstall"
	"github.com/jskswamy/aide/internal/piinstall/testfixture"
	"github.com/jskswamy/aide/pkg/seatbelt/modules"
)

func TestPiManagedGlobSymlinkDenial(t *testing.T) {
	f := testfixture.New(t)
	testfixture.Write(t, filepath.Join(f.Root, "private.txt"), "secret", 0600)
	testfixture.Link(t, f.Root, filepath.Join(f.Home, "aliases", "managed"))
	denied := filepath.Join(f.Home, "aliases", "*", "private.txt")
	p := Policy{HomeDir: f.Home, ExtraDenied: []string{denied}}
	if err := ValidatePiManagedInstall(p, piinstall.Installation{Root: f.Root}, BackendLandlock); err == nil {
		t.Fatal("glob matching a symlinked installation descendant was not rejected")
	}
}

func TestPiManagedCleanEnvPreservesXDG(t *testing.T) {
	f := testfixture.New(t)
	data := filepath.Join(f.Home, "custom-data")
	node := testfixture.Node(t, data)
	env := []string{"HOME=" + f.Home, "XDG_DATA_HOME=" + data, "PI_CODING_AGENT_DIR=" + filepath.Join(f.Home, ".pi-work")}
	in, err := piinstall.Discover(f.Launcher, f.Home, env, nil)
	if err != nil {
		t.Fatal(err)
	}
	p := Policy{HomeDir: f.Home, Env: env, CleanEnv: true, AgentModule: modules.PiAgentWithInstallation(in.Root, in.Readable)}
	filtered := filterEnv(env, p)
	if !slices.Contains(filtered, "XDG_DATA_HOME="+data) {
		t.Fatalf("managed bootstrap environment lost XDG_DATA_HOME: %v", filtered)
	}
	after, err := piinstall.Discover(f.Launcher, f.Home, filtered, nil)
	if err != nil || !slices.Contains(after.Readable, node) {
		t.Fatalf("execution would use a different runtime: %v %v", after, err)
	}
}
