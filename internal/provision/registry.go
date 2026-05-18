package provision

import "sort"

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
// boolean reporting whether it was found.
func ProvisionerFor(agentName string) (Provisioner, bool) {
	p, ok := provisionerRegistry[agentName]
	return p, ok
}

// AllProvisioners returns every registered driver, sorted by name for
// deterministic iteration.
func AllProvisioners() []Provisioner {
	names := make([]string, 0, len(provisionerRegistry))
	for n := range provisionerRegistry {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]Provisioner, len(names))
	for i, n := range names {
		out[i] = provisionerRegistry[n]
	}
	return out
}

// resetRegistryForTest clears the registry. Test-only helper.
func resetRegistryForTest() { provisionerRegistry = map[string]Provisioner{} }
