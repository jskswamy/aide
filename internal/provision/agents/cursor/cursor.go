// Package cursor provides the provision.Provisioner driver for Cursor.
// This initial driver implements only HookInstaller; plugin and MCP
// support are out of scope.
package cursor

import (
	"fmt"

	"github.com/jskswamy/aide/internal/provision"
)

const agentName = "cursor"

// Driver implements provision.Provisioner for Cursor.
type Driver struct {
	provision.DriverBase
}

// New returns a Driver.
func New() *Driver {
	return &Driver{
		DriverBase: provision.DriverBase{Caps: provision.Capabilities{
			AgentName:     agentName,
			SupportsHooks: true,
		}},
	}
}

func init() {
	provision.RegisterProvisioner(New())
}

// MCPConfigPath is not supported for Cursor; returns empty string.
func (*Driver) MCPConfigPath(_ provision.Context) string { return "" }

// MCPHandler is not supported for Cursor; returns nil.
func (*Driver) MCPHandler(_ provision.Context) provision.MCPHandler { return nil }

// InstalledPlugins is not supported for Cursor; returns nil.
func (*Driver) InstalledPlugins(_ provision.Context) ([]provision.Plugin, error) { return nil, nil }

// InstallPlugin is not supported for Cursor.
func (*Driver) InstallPlugin(_ provision.Context, _ provision.Plugin) error {
	return fmt.Errorf("cursor: plugins not supported")
}

// UninstallPlugin is not supported for Cursor.
func (*Driver) UninstallPlugin(_ provision.Context, _ string) error {
	return fmt.Errorf("cursor: plugins not supported")
}

// InstalledMarketplaces is not supported for Cursor; returns nil.
func (*Driver) InstalledMarketplaces(_ provision.Context) ([]provision.Marketplace, error) {
	return nil, nil
}

// AddMarketplace is not supported for Cursor.
func (*Driver) AddMarketplace(_ provision.Context, _ provision.Marketplace) error {
	return fmt.Errorf("cursor: marketplaces not supported")
}

// RemoveMarketplace is not supported for Cursor.
func (*Driver) RemoveMarketplace(_ provision.Context, _ string) error {
	return fmt.Errorf("cursor: marketplaces not supported")
}
