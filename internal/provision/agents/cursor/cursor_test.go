package cursor_test

import (
	"testing"

	"github.com/jskswamy/aide/internal/provision"
	"github.com/jskswamy/aide/internal/provision/agents/cursor"
)

func TestCursorCapabilities(t *testing.T) {
	d := cursor.New()
	if d.Name() != "cursor" {
		t.Errorf("Name = %q, want cursor", d.Name())
	}
	if d.SupportsPlugins() {
		t.Error("cursor should not support plugins")
	}
	if d.SupportsMCP() {
		t.Error("cursor should not support MCP")
	}
	if !d.SupportsHooks() {
		t.Error("cursor should support hooks")
	}
	if got := d.MCPConfigPath(provision.Context{}); got != "" {
		t.Errorf("MCPConfigPath = %q, want empty", got)
	}
	if got := d.MCPHandler(provision.Context{}); got != nil {
		t.Errorf("MCPHandler = %v, want nil", got)
	}
}

func TestCursorInstalledPlugins(t *testing.T) {
	d := cursor.New()
	plugins, err := d.InstalledPlugins(provision.Context{})
	if err != nil {
		t.Fatalf("InstalledPlugins: %v", err)
	}
	if len(plugins) != 0 {
		t.Errorf("InstalledPlugins = %v, want empty", plugins)
	}
}

func TestCursorPluginOperationsReturnError(t *testing.T) {
	d := cursor.New()
	if err := d.InstallPlugin(provision.Context{}, provision.Plugin{}); err == nil {
		t.Error("InstallPlugin should return error for cursor")
	}
	if err := d.UninstallPlugin(provision.Context{}, "pkg"); err == nil {
		t.Error("UninstallPlugin should return error for cursor")
	}
}

func TestCursorMarketplaceOperations(t *testing.T) {
	d := cursor.New()
	mks, err := d.InstalledMarketplaces(provision.Context{})
	if err != nil {
		t.Fatalf("InstalledMarketplaces: %v", err)
	}
	if len(mks) != 0 {
		t.Errorf("InstalledMarketplaces = %v, want empty", mks)
	}
	if err := d.AddMarketplace(provision.Context{}, provision.Marketplace{}); err == nil {
		t.Error("AddMarketplace should return error for cursor")
	}
	if err := d.RemoveMarketplace(provision.Context{}, "mk"); err == nil {
		t.Error("RemoveMarketplace should return error for cursor")
	}
}
