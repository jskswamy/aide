// Shared capability resolution for sandbox policy building.
//
// Extracts the capability → sandbox merge logic that was previously
// inline in launcher.Launch() so that CLI commands (sandbox test,
// sandbox show, sandbox guards) produce the same effective policy
// as the actual agent launch path.

package sandbox

import (
	"github.com/jskswamy/aide/internal/capability"
	"github.com/jskswamy/aide/internal/config"
)

// ApplyOverrides merges external overrides (from capabilities or other
// sources) into a SandboxPolicy. A nil *cfg is replaced with an empty
// policy. The policy is mutated in place.
func ApplyOverrides(cfg **config.SandboxPolicy, overrides config.SandboxOverrides) {
	if *cfg == nil {
		*cfg = &config.SandboxPolicy{}
	}
	(*cfg).Unguard = append((*cfg).Unguard, overrides.Unguard...)
	(*cfg).ReadableExtra = append((*cfg).ReadableExtra, overrides.ReadableExtra...)
	(*cfg).WritableExtra = append((*cfg).WritableExtra, overrides.WritableExtra...)
	(*cfg).DeniedExtra = append((*cfg).DeniedExtra, overrides.DeniedExtra...)
}

// MergeCapNames combines context capabilities with --with flags and
// removes --without flags, returning the final list of capability names.
func MergeCapNames(contextCaps, withCaps, withoutCaps []string) []string {
	capNames := make([]string, len(contextCaps))
	copy(capNames, contextCaps)
	capNames = append(capNames, withCaps...)

	if len(withoutCaps) > 0 {
		blocked := make(map[string]bool, len(withoutCaps))
		for _, c := range withoutCaps {
			blocked[c] = true
		}
		var filtered []string
		for _, c := range capNames {
			if !blocked[c] {
				filtered = append(filtered, c)
			}
		}
		capNames = filtered
	}
	return capNames
}

// ResolveCapabilities resolves capability names against the config
// registry and returns the merged SandboxOverrides. Returns a nil
// Set and zero overrides when capNames is empty.
func ResolveCapabilities(capNames []string, cfg *config.Config) (*capability.Set, config.SandboxOverrides, error) {
	if len(capNames) == 0 {
		return nil, config.SandboxOverrides{}, nil
	}
	userDefined := capability.FromConfigDefs(cfg.Capabilities)
	registry := capability.MergedRegistry(userDefined)
	capSet, err := capability.ResolveAll(capNames, registry, cfg.NeverAllow, cfg.NeverAllowEnv)
	if err != nil {
		return nil, config.SandboxOverrides{}, err
	}
	return capSet, capSet.ToSandboxOverrides(), nil
}
