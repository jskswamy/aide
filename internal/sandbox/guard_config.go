package sandbox

import (
	"slices"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/pkg/seatbelt"
	"github.com/jskswamy/aide/pkg/seatbelt/guards"
)

// EffectiveGuards resolves the active guard set for a sandbox config.
// Returns the list of guard names that would be active with this config.
func EffectiveGuards(cfg *config.SandboxPolicy) []string {
	if cfg == nil {
		return guards.DefaultGuardNames()
	}
	// Use the same resolution logic as resolveGuards in policy.go
	names, _, _ := resolveGuards(cfg)
	if names == nil {
		return guards.DefaultGuardNames()
	}
	return names
}

// EnableGuard adds a guard to the config, handling state correctly.
// Returns a ValidationResult with errors or warnings.
func EnableGuard(cfg *config.SandboxPolicy, name string) *seatbelt.ValidationResult {
	r := &seatbelt.ValidationResult{}

	// Reject meta-guard names
	expanded := guards.ExpandGuardName(name)
	if len(expanded) > 1 {
		r.AddError("use concrete guard names, not meta-guard %q (expands to %d guards)", name, len(expanded))
		return r
	}

	// Validate guard exists
	if _, ok := guards.GuardByName(name); !ok {
		r.AddError("unknown guard %q", name)
		return r
	}

	// Check if already active
	active := EffectiveGuards(cfg)
	for _, a := range active {
		if a == name {
			r.AddWarning("guard %q is already enabled", name)
			return r
		}
	}

	// Clean up Unguard entries that block this guard (including meta-guard expansion)
	var newUnguard []string
	for _, u := range cfg.Unguard {
		expanded := guards.ExpandGuardName(u)
		if slices.Contains(expanded, name) {
			// This entry (or its expansion) covers our target — keep the others
			for _, e := range expanded {
				if e != name {
					newUnguard = append(newUnguard, e)
				}
			}
		} else {
			newUnguard = append(newUnguard, u)
		}
	}
	cfg.Unguard = newUnguard

	// After unguard cleanup, check if guard is now active (default guard case)
	nowActive := EffectiveGuards(cfg)
	for _, a := range nowActive {
		if a == name {
			return r // removing from unguard was sufficient
		}
	}

	// Still not active — add to the right field
	if len(cfg.Guards) > 0 {
		cfg.Guards = append(cfg.Guards, name)
	} else {
		cfg.GuardsExtra = append(cfg.GuardsExtra, name)
	}

	return r
}

// DisableGuard removes a guard from the config, handling state correctly.
// Returns a ValidationResult with errors or warnings.
func DisableGuard(cfg *config.SandboxPolicy, name string) *seatbelt.ValidationResult {
	r := &seatbelt.ValidationResult{}

	// Reject meta-guard names
	expanded := guards.ExpandGuardName(name)
	if len(expanded) > 1 {
		r.AddError("use concrete guard names, not meta-guard %q (expands to %d guards)", name, len(expanded))
		return r
	}

	// Validate guard exists
	g, ok := guards.GuardByName(name)
	if !ok {
		r.AddError("unknown guard %q", name)
		return r
	}

	// Cannot disable always guards
	if g.Type() == "always" {
		r.AddError("guard %q is always active and cannot be disabled", name)
		return r
	}

	// Check if already inactive
	active := EffectiveGuards(cfg)
	isActive := false
	for _, a := range active {
		if a == name {
			isActive = true
			break
		}
	}
	if !isActive {
		r.AddWarning("guard %q is already disabled", name)
		return r
	}

	// Remove from guards: if present
	if i := slices.Index(cfg.Guards, name); i >= 0 {
		cfg.Guards = slices.Delete(cfg.Guards, i, i+1)
		return r
	}

	// Remove from guards_extra: if present
	if i := slices.Index(cfg.GuardsExtra, name); i >= 0 {
		cfg.GuardsExtra = slices.Delete(cfg.GuardsExtra, i, i+1)
		return r
	}

	// Add to unguard:
	cfg.Unguard = append(cfg.Unguard, name)
	return r
}

