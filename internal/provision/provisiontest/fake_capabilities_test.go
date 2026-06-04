package provisiontest

import (
	"errors"
	"testing"

	"github.com/jskswamy/aide/internal/provision"
)

func TestFakeProvisionerCapabilityGetters(t *testing.T) {
	f := &FakeProvisioner{
		AgentName:      "myagent",
		SupportsPlug:   true,
		SupportsMCPCfg: true,
		RequireTTY:     true,
		MCPPath:        "/tmp/mcp.json",
		Shapes:         []provision.SourceShape{provision.ShapeURLDirect},
	}
	if f.Name() != "myagent" {
		t.Errorf("Name = %q", f.Name())
	}
	if !f.SupportsPlugins() {
		t.Error("SupportsPlugins must be true")
	}
	if !f.SupportsMCP() {
		t.Error("SupportsMCP must be true")
	}
	if !f.RequiresTTY() {
		t.Error("RequiresTTY must be true")
	}
	if got := f.MCPConfigPath(provision.Context{}); got != "/tmp/mcp.json" {
		t.Errorf("MCPConfigPath = %q", got)
	}
	if got := f.SupportedSourceShapes(); len(got) != 1 || got[0] != provision.ShapeURLDirect {
		t.Errorf("Shapes = %v", got)
	}
	// MCPHandlerValue is nil by default — call still returns nil safely.
	if got := f.MCPHandler(provision.Context{}); got != nil {
		t.Errorf("MCPHandler default = %v, want nil", got)
	}
	if f.SupportsHooks() {
		t.Error("SupportsHooks must be false when SupportsHooksCfg is not set")
	}
}

func TestFakeProvisionerSupportsHooks(t *testing.T) {
	f := &FakeProvisioner{SupportsHooksCfg: true}
	if !f.SupportsHooks() {
		t.Error("SupportsHooks must be true when SupportsHooksCfg is true")
	}
}

func TestFakeProvisionerInstalledPluginsAndMarketplaces(t *testing.T) {
	f := &FakeProvisioner{
		InstalledPluginList: []provision.Plugin{{Key: "p1", Name: "p1"}, {Key: "p2", Name: "p2"}},
		InstalledMarkets:    []provision.Marketplace{{Key: "m1", Source: "github:m1/r"}},
	}
	plugins, err := f.InstalledPlugins(provision.Context{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plugins) != 2 {
		t.Errorf("InstalledPlugins len = %d, want 2", len(plugins))
	}

	mks, err := f.InstalledMarketplaces(provision.Context{})
	if err != nil {
		t.Fatal(err)
	}
	if len(mks) != 1 || mks[0].Key != "m1" {
		t.Errorf("InstalledMarketplaces = %+v", mks)
	}
}

func TestFakeProvisionerErrorSurfaces(t *testing.T) {
	wantErr := errors.New("plugins-down")
	f := &FakeProvisioner{PluginsErr: wantErr}
	if _, err := f.InstalledPlugins(provision.Context{}); !errors.Is(err, wantErr) {
		t.Errorf("PluginsErr did not surface, got %v", err)
	}

	wantMkErr := errors.New("mk-down")
	f = &FakeProvisioner{MarketplacesErr: wantMkErr}
	if _, err := f.InstalledMarketplaces(provision.Context{}); !errors.Is(err, wantMkErr) {
		t.Errorf("MarketplacesErr did not surface, got %v", err)
	}
}
