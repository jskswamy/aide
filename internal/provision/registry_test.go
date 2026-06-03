package provision

import (
	"testing"
)

type stubProv struct{ name string }

func (s stubProv) Name() string                                         { return s.name }
func (stubProv) SupportsPlugins() bool                                  { return false }
func (stubProv) SupportsMCP() bool                                      { return false }
func (stubProv) RequiresTTY() bool                                      { return false }
func (stubProv) MCPConfigPath(_ Context) string                         { return "" }
func (stubProv) InstalledPlugins(_ Context) ([]Plugin, error)           { return nil, nil }
func (stubProv) InstallPlugin(_ Context, _ Plugin) error                { return nil }
func (stubProv) UninstallPlugin(_ Context, _ string) error              { return nil }
func (stubProv) MCPHandler(_ Context) MCPHandler                        { return nil }
func (stubProv) SupportedSourceShapes() []SourceShape                   { return nil }
func (stubProv) SupportsHooks() bool                                    { return false }
func (stubProv) InstalledMarketplaces(_ Context) ([]Marketplace, error) { return nil, nil }
func (stubProv) AddMarketplace(_ Context, _ Marketplace) error          { return nil }
func (stubProv) RemoveMarketplace(_ Context, _ string) error            { return nil }

// snapshotRegistry returns a copy of the current registry contents
// so a test can restore them in a t.Cleanup. Used by registry tests
// that need to mutate global state without bleeding into peers.
func snapshotRegistry() map[string]Provisioner {
	snap := make(map[string]Provisioner, len(provisionerRegistry))
	for k, v := range provisionerRegistry {
		snap[k] = v
	}
	return snap
}

func restoreRegistry(snap map[string]Provisioner) {
	provisionerRegistry = snap
}

func TestRegisterAndLookup(t *testing.T) {
	snap := snapshotRegistry()
	t.Cleanup(func() { restoreRegistry(snap) })
	resetRegistryForTest()
	RegisterProvisioner(stubProv{name: "alpha"})
	RegisterProvisioner(stubProv{name: "beta"})

	if _, ok := ProvisionerFor("alpha"); !ok {
		t.Error("alpha not found")
	}
	if _, ok := ProvisionerFor("missing"); ok {
		t.Error("missing should not be found")
	}
	all := AllProvisioners()
	if len(all) != 2 {
		t.Fatalf("want 2, got %d", len(all))
	}
	if all[0].Name() != "alpha" || all[1].Name() != "beta" {
		t.Errorf("sort order = %v", []string{all[0].Name(), all[1].Name()})
	}
}

func TestRegisterDuplicatePanics(t *testing.T) {
	snap := snapshotRegistry()
	t.Cleanup(func() { restoreRegistry(snap) })
	resetRegistryForTest()
	RegisterProvisioner(stubProv{name: "x"})
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on duplicate")
		}
	}()
	RegisterProvisioner(stubProv{name: "x"})
}
