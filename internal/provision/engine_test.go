package provision_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/provision"
)

// fakeProv records every call and lets tests inject errors.
type fakeProv struct {
	name            string
	supportsPlugins bool
	supportsMCP     bool
	mcpPath         string
	installed       []provision.Plugin
	installedMCP    map[string]provision.MCPServer

	installErr   error
	uninstallErr error
	mcpWriteErr  error

	shapes []provision.SourceShape

	installedMarkets []provision.Marketplace
	marketplaceErr   error

	called []string
}

func (f *fakeProv) Name() string                             { return f.name }
func (f *fakeProv) SupportsPlugins() bool                    { return f.supportsPlugins }
func (f *fakeProv) SupportsMCP() bool                        { return f.supportsMCP }
func (f *fakeProv) RequiresTTY() bool                        { return false }
func (f *fakeProv) MCPConfigPath(_ provision.Context) string { return f.mcpPath }
func (f *fakeProv) InstalledPlugins(_ provision.Context) ([]provision.Plugin, error) {
	return f.installed, nil
}
func (f *fakeProv) InstallPlugin(_ provision.Context, p provision.Plugin) error {
	// Record key plus the resolved Name (the engine may rewrite
	// plugin@repo to plugin@canonical post-marketplace-add).
	f.called = append(f.called, "install:"+p.Key+":"+p.Name)
	return f.installErr
}
func (f *fakeProv) UninstallPlugin(_ provision.Context, name string) error {
	f.called = append(f.called, "uninstall:"+name)
	return f.uninstallErr
}
func (f *fakeProv) MCPHandler(_ provision.Context) provision.MCPHandler { return nil }
func (f *fakeProv) SupportedSourceShapes() []provision.SourceShape {
	return f.shapes
}
func (f *fakeProv) InstalledMarketplaces(_ provision.Context) ([]provision.Marketplace, error) {
	return f.installedMarkets, nil
}
func (f *fakeProv) AddMarketplace(_ provision.Context, m provision.Marketplace) error {
	f.called = append(f.called, "add-marketplace:"+m.Key)
	return f.marketplaceErr
}
func (f *fakeProv) RemoveMarketplace(_ provision.Context, name string) error {
	f.called = append(f.called, "remove-marketplace:"+name)
	return nil
}

func TestSyncInstallsDeclaredPlugin(t *testing.T) {
	fp := &fakeProv{name: "claude", supportsPlugins: true}
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
	if len(fp.called) != 1 || fp.called[0] != "install:linear:linear" {
		t.Errorf("calls = %v", fp.called)
	}
	if res.Performed != 1 {
		t.Errorf("performed = %d, want 1", res.Performed)
	}
}

func TestSyncRollsBackOnPluginInstallFailure(t *testing.T) {
	fp := &fakeProv{name: "claude", supportsPlugins: true, installErr: errors.New("network down")}
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
	fp := &fakeProv{
		name:            "claude",
		supportsPlugins: true,
		supportsMCP:     true,
		shapes:          []provision.SourceShape{provision.ShapeMarketplace},
		// The driver-reported marketplace name differs from the repo key:
		// aide passes "steveyegge/beads" (the repo) to AddMarketplace,
		// claude assigns canonical name "beads-marketplace", and the
		// plugin install command must use that canonical name. The engine
		// must rewrite Plugin.Name accordingly.
		installedMarkets: []provision.Marketplace{
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
	if len(fp.called) < 2 {
		t.Fatalf("expected 2 calls, got %v", fp.called)
	}
	if fp.called[0] != "add-marketplace:steveyegge/beads" {
		t.Errorf("first call = %q, want add-marketplace", fp.called[0])
	}
	// Plugin Name should have been rewritten from "beads@steveyegge/beads"
	// to "beads@beads-marketplace" using the driver's installed-marketplace
	// canonical-name lookup.
	if fp.called[1] != "install:beads:beads@beads-marketplace" {
		t.Errorf("second call = %q, want install:beads:beads@beads-marketplace (canonical-name rewrite)", fp.called[1])
	}
}

func TestSyncCapabilityMismatchErrors(t *testing.T) {
	fp := &fakeProv{name: "aider", supportsPlugins: false}
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
