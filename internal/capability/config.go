package capability

import "github.com/jskswamy/aide/internal/config"

// FromConfigDefs converts YAML config capability definitions to Capability types.
func FromConfigDefs(defs map[string]config.CapabilityDef) map[string]Capability {
	out := make(map[string]Capability, len(defs))
	for name, def := range defs {
		out[name] = Capability{
			Name:        name,
			Description: def.Description,
			Extends:     def.Extends,
			Combines:    def.Combines,
			Readable:    def.Readable,
			Writable:    def.Writable,
			Deny:        def.Deny,
			EnvAllow:    def.EnvAllow,
			Allow:       def.Allow,
		}
	}
	return out
}
