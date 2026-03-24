package guards_test

import (
	"strings"
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt"
	"github.com/jskswamy/aide/pkg/seatbelt/guards"
)

func TestMountedVolumes_Metadata(t *testing.T) {
	g := guards.MountedVolumesGuard()

	if g.Name() != "mounted-volumes" {
		t.Errorf("expected name mounted-volumes, got %s", g.Name())
	}
	if g.Type() != "default" {
		t.Errorf("expected type default, got %s", g.Type())
	}
}

func TestMountedVolumes_DeniesVolumes(t *testing.T) {
	g := guards.MountedVolumesGuard()
	ctx := &seatbelt.Context{HomeDir: "/Users/testuser"}
	result := g.Rules(ctx)

	if guards.TestDirExists("/Volumes") {
		output := renderTestRules(result.Rules)
		if !strings.Contains(output, `deny file-read-data`) {
			t.Error("expected deny rule for /Volumes")
		}
		if !strings.Contains(output, `"/Volumes"`) {
			t.Error("expected /Volumes path in deny rule")
		}
		if len(result.Protected) == 0 {
			t.Error("expected Protected to contain /Volumes")
		}
	} else {
		if len(result.Rules) != 0 {
			t.Error("expected no rules when /Volumes not found")
		}
		if len(result.Skipped) == 0 {
			t.Error("expected Skipped message when /Volumes not found")
		}
	}
}
