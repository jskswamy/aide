// Package hermes provides the provision.Provisioner driver for Hermes.
// This initial driver implements only HookInstaller; plugin and MCP
// support are out of scope.
package hermes

import (
	"github.com/jskswamy/aide/internal/provision"
)

const agentName = "hermes"

// Driver implements provision.Provisioner for Hermes.
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
