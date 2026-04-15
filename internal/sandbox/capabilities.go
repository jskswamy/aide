// Shared capability resolution for sandbox policy building.
//
// Extracts the capability → sandbox merge logic that was previously
// inline in launcher.Launch() so that CLI commands (sandbox test,
// sandbox show, sandbox guards) produce the same effective policy
// as the actual agent launch path.

package sandbox

import (
	"fmt"
	"io/fs"

	"github.com/jskswamy/aide/internal/capability"
	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/consent"
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
	(*cfg).GuardsExtra = append((*cfg).GuardsExtra, overrides.EnableGuard...)
	(*cfg).Allow = append((*cfg).Allow, overrides.Allow...)
	if overrides.NetworkMode != "" {
		if (*cfg).Network == nil {
			(*cfg).Network = &config.NetworkPolicy{}
		}
		(*cfg).Network.Mode = overrides.NetworkMode
	}
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

// VariantSelectionOptions carries everything the variant-aware
// resolver needs beyond the plain (capNames, cfg) pair. Zero value
// is valid — it disables detection and consent, and produces the
// same result as the plain ResolveCapabilities.
type VariantSelectionOptions struct {
	ProjectRoot  string
	FS           fs.FS               // propagates into SelectInput.FS; nil falls back to os.DirFS(ProjectRoot)
	CLIOverrides map[string][]string // from --variant flag
	YAMLPins     map[string][]string // from .aide.yaml capability_variants
	Consent      *consent.Store
	Prompter     capability.Prompter
	Interactive  bool
	AutoYes      bool
}

// ResolveCapabilitiesWithVariants resolves capNames, then for each
// capability that declares Variants runs capability.SelectVariants,
// merges the chosen variants' paths onto the resolved capability, and
// returns the resulting SandboxOverrides.
func ResolveCapabilitiesWithVariants(capNames []string, cfg *config.Config, opts VariantSelectionOptions) (*capability.Set, config.SandboxOverrides, map[string]capability.Provenance, error) {
	if len(capNames) == 0 {
		return nil, config.SandboxOverrides{}, nil, nil
	}
	userDefined := capability.FromConfigDefs(cfg.Capabilities)
	registry := capability.MergedRegistry(userDefined)
	capSet, err := capability.ResolveAll(capNames, registry, cfg.NeverAllow, cfg.NeverAllowEnv)
	if err != nil {
		return nil, config.SandboxOverrides{}, nil, err
	}

	provenance := make(map[string]capability.Provenance)
	// For each resolved capability, if its defining built-in (or user
	// def) has Variants, run selection and replace the resolved entry
	// with the variant-merged version.
	for i := range capSet.Capabilities {
		rc := &capSet.Capabilities[i]
		def, ok := registry[rc.Name]
		if !ok || len(def.Variants) == 0 {
			continue
		}
		selected, prov, selErr := capability.SelectVariants(capability.SelectInput{
			Capability:  def,
			ProjectRoot: opts.ProjectRoot,
			FS:          opts.FS,
			Overrides:   opts.CLIOverrides[rc.Name],
			YAMLPins:    opts.YAMLPins[rc.Name],
			Consent:     opts.Consent,
			Prompter:    opts.Prompter,
			Interactive: opts.Interactive,
			AutoYes:     opts.AutoYes,
		})
		if selErr != nil {
			return nil, config.SandboxOverrides{}, nil, fmt.Errorf("selecting variants for %q: %w", rc.Name, selErr)
		}
		provenance[rc.Name] = prov
		merged := capability.MergeSelectedVariants(rc, selected)
		capSet.Capabilities[i] = *merged
	}
	return capSet, capSet.ToSandboxOverrides(), provenance, nil
}

// ResolveCapabilities is a backward-compatible wrapper that runs the
// variant-aware resolver with zero options (no detection, no consent).
// Retained so existing callers that do not yet handle variants keep
// working unchanged.
func ResolveCapabilities(capNames []string, cfg *config.Config) (*capability.Set, config.SandboxOverrides, error) {
	set, overrides, _, err := ResolveCapabilitiesWithVariants(capNames, cfg, VariantSelectionOptions{})
	return set, overrides, err
}
