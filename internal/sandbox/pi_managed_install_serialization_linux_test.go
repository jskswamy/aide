//go:build linux

package sandbox

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/jskswamy/aide/internal/piinstall/testfixture"
	"github.com/jskswamy/aide/pkg/seatbelt/modules"
)

func TestPiManagedInstallPolicyRoundTrip(t *testing.T) {
	f := testfixture.New(t)
	work := filepath.Join(f.Home, ".pi-work")
	p := DefaultPolicy(Paths{HomeDir: f.Home}, []string{"PI_CODING_AGENT_DIR=" + work})
	p.AgentModule = modules.PiAgentWithInstallation(f.Root, []string{f.Launcher})
	encoded, err := policyToJSON(p)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(encoded.AgentWritable, f.Root) || !slices.Contains(encoded.AgentWritable, work) || !slices.Contains(encoded.AgentReadable, f.Launcher) {
		t.Fatalf("module paths lost: %+v", encoded)
	}
	decoded := policyFromJSON(encoded)
	grants := DeriveGrantedPathSet(decoded)
	if !slices.Contains(grants.Writable, f.Root) || !slices.Contains(grants.Readable, f.Launcher) {
		t.Fatalf("re-exec grants lost: %+v", grants)
	}
	if slices.Contains(grants.Writable, f.Agent) || slices.Contains(grants.Writable, f.Launcher) {
		t.Fatal("bootstrap or private parent became writable")
	}
}
