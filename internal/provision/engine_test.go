package provision_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/provision"
	"github.com/jskswamy/aide/internal/provision/provisiontest"
)

func TestSyncInstallsDeclaredPlugin(t *testing.T) {
	fp := &provisiontest.FakeProvisioner{AgentName: "claude", SupportsPlug: true}
	desired := provision.Desired{
		Plugins: map[string]provision.Plugin{
			"linear": {Key: "linear", Source: "marketplace", Name: "linear"},
		},
	}
	plan := provision.ComputePlan(provision.Context{Name: "work", Agent: "claude"}, desired, provision.Installed{}, provision.ContextState{})
	res, err := provision.Apply(fp, plan, provision.ApplyOptions{})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(fp.Called) != 1 || fp.Called[0] != "install:linear:linear" {
		t.Errorf("calls = %v", fp.Called)
	}
	if res.Performed != 1 {
		t.Errorf("performed = %d, want 1", res.Performed)
	}
}

func TestSyncRollsBackOnPluginInstallFailure(t *testing.T) {
	fp := &provisiontest.FakeProvisioner{
		AgentName:    "claude",
		SupportsPlug: true,
		InstallErr:   errors.New("network down"),
	}
	desired := provision.Desired{
		Plugins: map[string]provision.Plugin{
			"a": {Key: "a", Source: "marketplace", Name: "a"},
			"b": {Key: "b", Source: "marketplace", Name: "b"},
		},
	}
	plan := provision.ComputePlan(provision.Context{Name: "work", Agent: "claude"}, desired, provision.Installed{}, provision.ContextState{})
	_, err := provision.Apply(fp, plan, provision.ApplyOptions{})
	if err == nil {
		t.Fatal("expected sync to fail")
	}
	if !strings.Contains(err.Error(), "install plugin") {
		t.Errorf("error %q should name failing op kind", err)
	}
}

func TestApplyAddsMarketplaceBeforePlugin(t *testing.T) {
	fp := &provisiontest.FakeProvisioner{
		AgentName:      "claude",
		SupportsPlug:   true,
		SupportsMCPCfg: true,
		Shapes:         []provision.SourceShape{provision.ShapeMarketplace},
		// The driver-reported marketplace name differs from the repo key:
		// aide passes "steveyegge/beads" (the repo) to AddMarketplace,
		// claude assigns canonical name "beads-marketplace", and the
		// plugin install command must use that canonical name. The engine
		// must rewrite Plugin.Name accordingly.
		InstalledMarkets: []provision.Marketplace{
			{Key: "steveyegge/beads", Source: "github:steveyegge/beads", Name: "beads-marketplace"},
		},
	}
	desired := provision.Desired{
		Marketplaces: map[string]provision.Marketplace{
			"steveyegge/beads": {Key: "steveyegge/beads", Source: "github:steveyegge/beads"},
		},
		Plugins: map[string]provision.Plugin{
			"beads": {Key: "beads", Name: "beads@steveyegge/beads", Source: "marketplace"},
		},
	}
	plan := provision.ComputePlan(provision.Context{Name: "t", Agent: "claude"}, desired, provision.Installed{}, provision.ContextState{})
	_, err := provision.Apply(fp, plan, provision.ApplyOptions{})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(fp.Called) < 2 {
		t.Fatalf("expected 2 calls, got %v", fp.Called)
	}
	if fp.Called[0] != "add-marketplace:steveyegge/beads" {
		t.Errorf("first call = %q, want add-marketplace", fp.Called[0])
	}
	// Plugin Name should have been rewritten from "beads@steveyegge/beads"
	// to "beads@beads-marketplace" using the driver's installed-marketplace
	// canonical-name lookup.
	if fp.Called[1] != "install:beads:beads@beads-marketplace" {
		t.Errorf("second call = %q, want install:beads:beads@beads-marketplace (canonical-name rewrite)", fp.Called[1])
	}
}

func TestSyncCapabilityMismatchErrors(t *testing.T) {
	fp := &provisiontest.FakeProvisioner{AgentName: "aider", SupportsPlug: false}
	desired := provision.Desired{
		Plugins: map[string]provision.Plugin{
			"x": {Key: "x", Source: "marketplace", Name: "x"},
		},
	}
	plan := provision.ComputePlan(provision.Context{Name: "work", Agent: "aider"}, desired, provision.Installed{}, provision.ContextState{})
	_, err := provision.Apply(fp, plan, provision.ApplyOptions{})
	if err == nil || !strings.Contains(err.Error(), "does not support plugins") {
		t.Errorf("expected capability error, got %v", err)
	}
}
