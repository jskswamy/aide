// Package cursor provides the provision.Provisioner driver for Cursor.
// This initial driver implements only HookInstaller; plugin and MCP
// support are out of scope.
package cursor

import (
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
