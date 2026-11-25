package provision

import (
	"github.com/jskswamy/aide/internal/harness"
)

// provisionerRegistry holds all drivers registered via
// RegisterProvisioner. Drivers register themselves in their package's
// init(); cmd/aide blank-imports them so wiring happens at link time.
var provisionerRegistry = map[string]Provisioner{}

// RegisterProvisioner adds a driver to the global registry. It panics
// on duplicate agent names so wiring mistakes surface at startup.
func RegisterProvisioner(p Provisioner) {
	name := p.Name()
	if _, dup := provisionerRegistry[name]; dup {
		panic("provision: duplicate provisioner registered for agent " + name)
	}
	provisionerRegistry[name] = p
}

// ProvisionerFor returns the driver for the given agent name and a
// boolean reporting whether it was found. A harness name or alias
// finds the driver registered under any of the harness's names, so
// "cursor-agent" finds the "cursor" driver.
func ProvisionerFor(agentName string) (Provisioner, bool) {
	if p, ok := provisionerRegistry[agentName]; ok {
		return p, true
	}
	d, ok := harness.Lookup(agentName)
	if !ok {
		return nil, false
	}
	for _, name := range d.Names() {
		if p, ok := provisionerRegistry[name]; ok {
			return p, true
		}
	}
	return nil, false
}
