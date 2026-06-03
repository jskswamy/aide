// Package provisiontest provides a shared, configurable
// test-double for provision.Provisioner so engine_test and cmd/aide
// tests don't each maintain their own hand-rolled fake.
//
// Two recording styles are supported simultaneously:
//   - The unified Called []string log (engine_test style).
//   - Per-method slices (InstallCalls, UninstallCall, AddedMarketplaces,
//     RemoveMarketplaces — cmd/aide style).
//
// Pluggable error-injection fields allow tests to simulate runner
// failures without wiring a fake Runner.
package provisiontest

import "github.com/jskswamy/aide/internal/provision"

// FakeProvisioner is a configurable test double implementing
// provision.Provisioner. Fields are exported so tests can set up
// state and read back recorded calls directly. Reset via the Reset
// method between tests.
type FakeProvisioner struct {
	// Identity / capability advertising.
	AgentName        string
	SupportsPlug     bool
	SupportsMCPCfg   bool
	RequireTTY       bool
	Shapes           []provision.SourceShape
	SupportsHooksCfg bool
	MCPPath          string
	MCPHandlerValue  provision.MCPHandler

	// State the driver reports back.
	InstalledPluginList []provision.Plugin
	InstalledMarkets    []provision.Marketplace

	// Error injection.
	PluginsErr        error
	InstallErr        error
	UninstallErr      error
	MarketplacesErr   error
	AddMarketplaceErr error
	MCPWriteErr       error

	// Per-method recording (cmd/aide style).
	InstallCalls       []provision.Plugin
	UninstallCall      []string
	AddedMarketplaces  []provision.Marketplace
	RemoveMarketplaces []string

	// Unified call-log recording (engine_test style).
	Called []string
}

// Reset clears recorded state and error injection, preserving the
// identity/capability fields the caller set up.
func (f *FakeProvisioner) Reset() {
	f.InstalledPluginList = nil
	f.InstalledMarkets = nil
	f.PluginsErr = nil
	f.InstallErr = nil
	f.UninstallErr = nil
	f.MarketplacesErr = nil
	f.AddMarketplaceErr = nil
	f.MCPWriteErr = nil
	f.InstallCalls = nil
	f.UninstallCall = nil
	f.AddedMarketplaces = nil
	f.RemoveMarketplaces = nil
	f.Called = nil
}

// Name implements provision.Provisioner.
func (f *FakeProvisioner) Name() string { return f.AgentName }

// SupportsPlugins implements provision.Provisioner.
func (f *FakeProvisioner) SupportsPlugins() bool { return f.SupportsPlug }

// SupportsMCP implements provision.Provisioner.
func (f *FakeProvisioner) SupportsMCP() bool { return f.SupportsMCPCfg }

// RequiresTTY implements provision.Provisioner.
func (f *FakeProvisioner) RequiresTTY() bool { return f.RequireTTY }

// MCPConfigPath implements provision.Provisioner.
func (f *FakeProvisioner) MCPConfigPath(_ provision.Context) string { return f.MCPPath }

// MCPHandler implements provision.Provisioner.
func (f *FakeProvisioner) MCPHandler(_ provision.Context) provision.MCPHandler {
	return f.MCPHandlerValue
}

// SupportedSourceShapes implements provision.Provisioner.
func (f *FakeProvisioner) SupportedSourceShapes() []provision.SourceShape { return f.Shapes }

// SupportsHooks implements provision.Provisioner.
func (f *FakeProvisioner) SupportsHooks() bool { return f.SupportsHooksCfg }

// InstalledPlugins implements provision.Provisioner.
func (f *FakeProvisioner) InstalledPlugins(_ provision.Context) ([]provision.Plugin, error) {
	if f.PluginsErr != nil {
		return nil, f.PluginsErr
	}
	return f.InstalledPluginList, nil
}

// InstallPlugin implements provision.Provisioner.
func (f *FakeProvisioner) InstallPlugin(_ provision.Context, p provision.Plugin) error {
	f.InstallCalls = append(f.InstallCalls, p)
	f.Called = append(f.Called, "install:"+p.Key+":"+p.Name)
	return f.InstallErr
}

// UninstallPlugin implements provision.Provisioner.
func (f *FakeProvisioner) UninstallPlugin(_ provision.Context, name string) error {
	f.UninstallCall = append(f.UninstallCall, name)
	f.Called = append(f.Called, "uninstall:"+name)
	return f.UninstallErr
}

// InstalledMarketplaces implements provision.Provisioner.
func (f *FakeProvisioner) InstalledMarketplaces(_ provision.Context) ([]provision.Marketplace, error) {
	return f.InstalledMarkets, f.MarketplacesErr
}

// AddMarketplace implements provision.Provisioner.
func (f *FakeProvisioner) AddMarketplace(_ provision.Context, m provision.Marketplace) error {
	f.AddedMarketplaces = append(f.AddedMarketplaces, m)
	f.Called = append(f.Called, "add-marketplace:"+m.Key)
	return f.AddMarketplaceErr
}

// RemoveMarketplace implements provision.Provisioner.
func (f *FakeProvisioner) RemoveMarketplace(_ provision.Context, name string) error {
	f.RemoveMarketplaces = append(f.RemoveMarketplaces, name)
	f.Called = append(f.Called, "remove-marketplace:"+name)
	return nil
}

// FakeProvisionerWithMCPCLI extends FakeProvisioner with the
// MCPInstaller interface — i.e. CLI-driven MCP management. Use this
// in tests that exercise the engine's MCPInstaller dispatch
// (claude/gemini/codex pattern). FakeProvisioner alone keeps the
// older file-handler path so existing tests are unaffected.
type FakeProvisionerWithMCPCLI struct {
	*FakeProvisioner

	// State the driver reports back when InstalledMCPServers is queried.
	InstalledMCP map[string]provision.MCPServer

	// Error injection for the three MCPInstaller methods.
	InstalledMCPErr error
	InstallMCPErr   error
	UninstallMCPErr error

	// Per-method recording.
	InstalledMCPQuery [][]string // each call's `names` slice
	InstallMCPCalls   []provision.MCPServer
	UninstallMCPCalls []string
}

// InstalledMCPServers implements provision.MCPInstaller.
func (f *FakeProvisionerWithMCPCLI) InstalledMCPServers(_ provision.Context, names []string) (map[string]provision.MCPServer, error) {
	cp := append([]string{}, names...)
	f.InstalledMCPQuery = append(f.InstalledMCPQuery, cp)
	if f.InstalledMCPErr != nil {
		return nil, f.InstalledMCPErr
	}
	out := map[string]provision.MCPServer{}
	for _, n := range names {
		if v, ok := f.InstalledMCP[n]; ok {
			out[n] = v
		}
	}
	return out, nil
}

// InstallMCPServer implements provision.MCPInstaller.
func (f *FakeProvisionerWithMCPCLI) InstallMCPServer(_ provision.Context, s provision.MCPServer) error {
	f.InstallMCPCalls = append(f.InstallMCPCalls, s)
	f.Called = append(f.Called, "install-mcp:"+s.Key)
	return f.InstallMCPErr
}

// UninstallMCPServer implements provision.MCPInstaller.
func (f *FakeProvisionerWithMCPCLI) UninstallMCPServer(_ provision.Context, name string) error {
	f.UninstallMCPCalls = append(f.UninstallMCPCalls, name)
	f.Called = append(f.Called, "uninstall-mcp:"+name)
	return f.UninstallMCPErr
}

// FakeProvisionerWithHookInstaller extends FakeProvisioner with HookInstaller.
type FakeProvisionerWithHookInstaller struct {
	*FakeProvisioner
	StoredHooks     []provision.Hook
	ReadHooksErr    error
	WriteHooksErr   error
	WriteHooksCalls [][]provision.Hook // each call's desired slice
}

// ReadHooks implements provision.HookInstaller.
func (f *FakeProvisionerWithHookInstaller) ReadHooks(_ provision.Context) ([]provision.Hook, error) {
	return append([]provision.Hook{}, f.StoredHooks...), f.ReadHooksErr
}

// WriteHooks implements provision.HookInstaller.
func (f *FakeProvisionerWithHookInstaller) WriteHooks(_ provision.Context, prevManaged []provision.Hook, desired []provision.Hook) error {
	if f.WriteHooksErr != nil {
		return f.WriteHooksErr
	}
	// Remove prevManaged from StoredHooks, then add desired.
	removeSet := map[string]bool{}
	for _, h := range prevManaged {
		removeSet[provision.HookKey(h.Event, h.Matcher, h.Command)] = true
	}
	var kept []provision.Hook
	for _, h := range f.StoredHooks {
		if !removeSet[provision.HookKey(h.Event, h.Matcher, h.Command)] {
			kept = append(kept, h)
		}
	}
	f.StoredHooks = kept
	f.StoredHooks = append(f.StoredHooks, desired...)
	cp := append([]provision.Hook{}, desired...)
	f.WriteHooksCalls = append(f.WriteHooksCalls, cp)
	return nil
}
