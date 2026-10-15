package provision

import (
	"fmt"
	"slices"
)

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
	// PluginsByInstallRef marks drivers whose agent reports and removes
	// URL-direct plugins by install ref (OpenCode, Pi) rather than by a
	// name matching the config key (Gemini). See InstallRefAddresser.
	PluginsByInstallRef bool
	// PluginNoun is what the agent calls its plugins ("extensions",
	// "packages"), used in user-facing errors. Empty means "plugins".
	PluginNoun string
}

// DriverBase is embeddable in per-agent Driver structs. It carries
// the Capabilities and promotes the five trivial Provisioner methods
// that previously lived as one-line stubs in every driver.
type DriverBase struct {
	Caps Capabilities
}

// PluginsByInstallRef implements InstallRefAddresser.
func (d DriverBase) PluginsByInstallRef() bool { return d.Caps.PluginsByInstallRef }

// ProfileEnvKey returns the env var that relocates the agent's config
// dir, or "" when the agent has none.
func (d DriverBase) ProfileEnvKey() string { return d.Caps.ProfileEnvKey }

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

// AddMarketplace is not supported for stub-only drivers. Agents that
// only take URL-direct sources have no marketplace concept, so for them
// it is a config error pointing at the inline form.
func (d DriverBase) AddMarketplace(_ Context, _ Marketplace) error {
	if d.urlDirectOnly() {
		noun := d.Caps.PluginNoun
		if noun == "" {
			noun = "plugins"
		}
		return fmt.Errorf("%s does not have marketplaces; declare %s inline with string values", d.Caps.AgentName, noun)
	}
	return d.unsupported("marketplaces")
}

// RemoveMarketplace is not supported for stub-only drivers. For agents
// that only take URL-direct sources it is a no-op, since there is never
// a marketplace to remove and install rollback must not fail on it.
func (d DriverBase) RemoveMarketplace(_ Context, _ string) error {
	if d.urlDirectOnly() {
		return nil
	}
	return d.unsupported("marketplaces")
}

// urlDirectOnly reports whether the driver accepts URL-direct plugin
// sources and no marketplace ones.
func (d DriverBase) urlDirectOnly() bool {
	return slices.Contains(d.Caps.SourceShapes, ShapeURLDirect) &&
		!slices.Contains(d.Caps.SourceShapes, ShapeMarketplace)
}

// unsupported returns an error for unsupported features, using the driver's agent name.
func (d DriverBase) unsupported(feature string) error {
	return fmt.Errorf("%s: %s not supported", d.Caps.AgentName, feature)
}
