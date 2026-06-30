package provision

import "fmt"

// Capabilities is the static capability/identity block every
// Provisioner driver carries. Drivers populate it once in their
// constructor; DriverBase promotes it to the six trivial methods
// Provisioner requires (Name, SupportsPlugins, SupportsMCP,
// RequiresTTY, SupportedSourceShapes, SupportsHooks).
//
// Adding a new capability bit (e.g. SupportsHooks) means extending
// this struct and DriverBase once, rather than touching every
// per-agent package.
type Capabilities struct {
	AgentName       string
	SupportsPlugins bool
	SupportsMCP     bool
	RequiresTTY     bool
	SourceShapes    []SourceShape
	SupportsHooks   bool
	// ProfileEnvKey is the env-var name the driver injects when a
	// context declares profile:. Empty string signals the driver does
	// not support profile (see DriverBase.Profile, which returns
	// ErrProfileNotSupported in that case).
	ProfileEnvKey string
}

// DriverBase is embeddable in per-agent Driver structs. It carries
// the Capabilities and promotes the five trivial Provisioner methods
// that previously lived as one-line stubs in every driver.
type DriverBase struct {
	Caps Capabilities
}

// Name implements Provisioner.
func (d DriverBase) Name() string { return d.Caps.AgentName }

// SupportsPlugins implements Provisioner.
func (d DriverBase) SupportsPlugins() bool { return d.Caps.SupportsPlugins }

// SupportsMCP implements Provisioner.
func (d DriverBase) SupportsMCP() bool { return d.Caps.SupportsMCP }

// RequiresTTY implements Provisioner.
func (d DriverBase) RequiresTTY() bool { return d.Caps.RequiresTTY }

// SupportedSourceShapes implements Provisioner.
func (d DriverBase) SupportedSourceShapes() []SourceShape { return d.Caps.SourceShapes }

// SupportsHooks implements Provisioner.
func (d DriverBase) SupportsHooks() bool { return d.Caps.SupportsHooks }

// MCPConfigPath is not supported for stub-only drivers; returns empty string.
func (d DriverBase) MCPConfigPath(_ Context) string { return "" }

// MCPHandler is not supported for stub-only drivers; returns nil.
func (d DriverBase) MCPHandler(_ Context) MCPHandler { return nil }

// InstalledPlugins is not supported for stub-only drivers; returns nil.
func (d DriverBase) InstalledPlugins(_ Context) ([]Plugin, error) { return nil, nil }

// InstallPlugin is not supported for stub-only drivers.
func (d DriverBase) InstallPlugin(_ Context, _ Plugin) error {
	return d.unsupported("plugins")
}

// UninstallPlugin is not supported for stub-only drivers.
func (d DriverBase) UninstallPlugin(_ Context, _ string) error {
	return d.unsupported("plugins")
}

// InstalledMarketplaces is not supported for stub-only drivers; returns nil.
func (d DriverBase) InstalledMarketplaces(_ Context) ([]Marketplace, error) {
	return nil, nil
}

// AddMarketplace is not supported for stub-only drivers.
func (d DriverBase) AddMarketplace(_ Context, _ Marketplace) error {
	return d.unsupported("marketplaces")
}

// RemoveMarketplace is not supported for stub-only drivers.
func (d DriverBase) RemoveMarketplace(_ Context, _ string) error {
	return d.unsupported("marketplaces")
}

// unsupported returns an error for unsupported features, using the driver's agent name.
func (d DriverBase) unsupported(feature string) error {
	return fmt.Errorf("%s: %s not supported", d.Caps.AgentName, feature)
}
