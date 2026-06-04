package hermes_test

import (
	"testing"

	"github.com/jskswamy/aide/internal/provision"
	"github.com/jskswamy/aide/internal/provision/agents/hermes"
)

func TestHermesCapabilities(t *testing.T) {
	d := hermes.New()
	if d.Name() != "hermes" {
		t.Errorf("Name = %q, want hermes", d.Name())
	}
	if d.SupportsPlugins() {
		t.Error("hermes should not support plugins")
	}
	if d.SupportsMCP() {
		t.Error("hermes should not support MCP")
	}
	if !d.SupportsHooks() {
		t.Error("hermes should support hooks")
	}
	if got := d.MCPConfigPath(provision.Context{}); got != "" {
		t.Errorf("MCPConfigPath = %q, want empty", got)
	}
	if got := d.MCPHandler(provision.Context{}); got != nil {
		t.Errorf("MCPHandler = %v, want nil", got)
	}
}

func TestHermesInstalledPlugins(t *testing.T) {
	d := hermes.New()
	plugins, err := d.InstalledPlugins(provision.Context{})
	if err != nil {
		t.Fatalf("InstalledPlugins: %v", err)
	}
	if len(plugins) != 0 {
		t.Errorf("InstalledPlugins = %v, want empty", plugins)
	}
}

func TestHermesPluginOperationsReturnError(t *testing.T) {
	d := hermes.New()
	if err := d.InstallPlugin(provision.Context{}, provision.Plugin{}); err == nil {
		t.Error("InstallPlugin should return error for hermes")
	}
	if err := d.UninstallPlugin(provision.Context{}, "pkg"); err == nil {
		t.Error("UninstallPlugin should return error for hermes")
	}
}

func TestHermesMarketplaceOperations(t *testing.T) {
	d := hermes.New()
	mks, err := d.InstalledMarketplaces(provision.Context{})
	if err != nil {
		t.Fatalf("InstalledMarketplaces: %v", err)
	}
	if len(mks) != 0 {
		t.Errorf("InstalledMarketplaces = %v, want empty", mks)
	}
	if err := d.AddMarketplace(provision.Context{}, provision.Marketplace{}); err == nil {
		t.Error("AddMarketplace should return error for hermes")
	}
	if err := d.RemoveMarketplace(provision.Context{}, "mk"); err == nil {
		t.Error("RemoveMarketplace should return error for hermes")
	}
}
