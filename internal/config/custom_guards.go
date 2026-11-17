package config

import (
	"fmt"
	"maps"
	"slices"

	"github.com/jskswamy/aide/pkg/seatbelt/guards"
)

// registerCustomGuards resolves each custom guard's type through
// guard_types and registers the set with the seatbelt guard registry, so
// every sandbox lookup (guards, guards_extra, unguard, defaults) sees it.
func registerCustomGuards(c *Config) error {
	for _, name := range slices.Sorted(maps.Keys(c.GuardTypes)) {
		if slices.Contains([]string{"always", "default", "opt-in"}, name) {
			return fmt.Errorf("guard_types.%s: cannot redefine a built-in guard type", name)
		}
		if b := c.GuardTypes[name].Behavior; b != "default" && b != "opt-in" {
			return fmt.Errorf("guard_types.%s: behavior must be default or opt-in, got %q", name, b)
		}
	}
	defs := make(map[string]guards.CustomGuardConfig, len(c.CustomGuards))
	for name, g := range c.CustomGuards {
		typ := g.Type
		if gt, ok := c.GuardTypes[typ]; ok {
			typ = gt.Behavior
		}
		defs[name] = guards.CustomGuardConfig{Type: typ, Description: g.Description, Paths: g.Paths, EnvOverride: g.EnvOverride, Allowed: g.Allowed}
	}
	if err := guards.SetCustomGuards(defs); err != nil {
		return fmt.Errorf("custom_guards: %w", err)
	}
	return nil
}
