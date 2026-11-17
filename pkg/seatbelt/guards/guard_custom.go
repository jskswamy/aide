// Custom guard support for macOS Seatbelt profiles.
//
// NewCustomGuard builds a Guard from user-supplied configuration, allowing
// projects to protect arbitrary paths without writing Go code.

package guards

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"sync"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

// CustomGuardConfig holds the configuration for a user-defined guard.
type CustomGuardConfig struct {
	// Type is the guard type: "default" or "opt-in". "always" is not allowed.
	Type string
	// Description is shown in CLI output.
	Description string
	// Paths is the list of paths to deny (may start with "~/").
	Paths []string
	// EnvOverride is an optional environment variable that, when set to a
	// single path, replaces the default Paths list.
	EnvOverride string
	// Allowed is a list of paths for which (allow file-read* (literal "..."))
	// rules are emitted.
	Allowed []string
}

type customGuard struct {
	name string
	cfg  CustomGuardConfig
}

// NewCustomGuard creates a Guard from the supplied name and configuration.
// No validation is performed; call ValidateCustomGuard first if needed.
func NewCustomGuard(name string, cfg CustomGuardConfig) seatbelt.Guard {
	return &customGuard{name: name, cfg: cfg}
}

func (g *customGuard) Name() string        { return g.name }
func (g *customGuard) Type() string        { return cmp.Or(g.cfg.Type, "default") }
func (g *customGuard) Description() string { return g.cfg.Description }

func (g *customGuard) Rules(ctx *seatbelt.Context) seatbelt.GuardResult {
	if ctx == nil || ctx.HomeDir == "" {
		return seatbelt.GuardResult{}
	}
	result := seatbelt.GuardResult{}

	// Check for env override and record it.
	if g.cfg.EnvOverride != "" {
		if v, ok := ctx.EnvLookup(g.cfg.EnvOverride); ok && v != "" {
			parts := SplitColonPaths(v)
			if len(parts) == 1 {
				defaultPath := ""
				if len(g.cfg.Paths) == 1 {
					defaultPath = seatbelt.ExpandHome(g.cfg.Paths[0], ctx.HomeDir)
				}
				result.Overrides = append(result.Overrides, seatbelt.Override{
					EnvVar:      g.cfg.EnvOverride,
					Value:       v,
					DefaultPath: defaultPath,
				})
			}
		}
	}

	// Allow rules and opt-out set for explicitly allowed paths.
	allowedSet := make(map[string]bool, len(g.cfg.Allowed)*2)
	for _, a := range g.cfg.Allowed {
		expanded := filepath.Clean(seatbelt.ExpandHome(a, ctx.HomeDir))
		result.Rules = append(result.Rules, AllowReadFile(expanded))
		result.Allowed = append(result.Allowed, expanded)
		allowedSet[expanded] = true
		allowedSet[seatbelt.ResolveOrSelf(expanded)] = true
	}

	paths := g.resolvePaths(ctx)

	// Deny rules for each path, with existence check and symlink resolution.
	// macOS Seatbelt checks file-read* against the kernel-resolved target, so
	// a symlinked key must also have its resolved target denied.
	seen := make(map[string]bool, len(paths)*2)
	protect := func(target string) {
		if !seen[target] && !allowedSet[target] {
			seen[target] = true
			result.Rules = append(result.Rules, DenyDir(target)...)
			result.Protected = append(result.Protected, target)
		}
	}
	for _, p := range paths {
		resolved := seatbelt.ResolveOrSelf(p)
		if allowedSet[p] || allowedSet[resolved] {
			continue
		}
		if pathExists(p) {
			protect(p)
			if resolved != p {
				protect(resolved)
			}
		} else {
			result.Skipped = append(result.Skipped, fmt.Sprintf("%s not found", p))
		}
	}

	return result
}

// resolvePaths returns the effective list of absolute paths for the guard,
// applying home-dir expansion and optional env-override.
func (g *customGuard) resolvePaths(ctx *seatbelt.Context) []string {
	// If EnvOverride is set and the env var resolves to a single path, use it.
	if g.cfg.EnvOverride != "" {
		if v, ok := ctx.EnvLookup(g.cfg.EnvOverride); ok && v != "" {
			parts := SplitColonPaths(v)
			if len(parts) == 1 {
				return parts
			}
		}
	}

	var out []string
	for _, p := range g.cfg.Paths {
		p = seatbelt.ExpandHome(p, ctx.HomeDir)
		// Globs expand at launch; files created later need a relaunch.
		if expanded := seatbelt.ExpandGlobs([]string{p}); len(expanded) > 0 {
			out = append(out, expanded...)
			continue
		}
		out = append(out, p) // glob with no matches (reported as skipped)
	}
	return out
}

// ValidateCustomGuard checks the configuration for common mistakes.
// Returns a ValidationResult; check result.OK() to determine if validation passed.
func ValidateCustomGuard(name string, cfg CustomGuardConfig) *seatbelt.ValidationResult {
	r := &seatbelt.ValidationResult{}
	switch cfg.Type {
	case "", "default", "opt-in":
	case "always":
		r.AddError("custom guard %q cannot use type \"always\"", name)
	default:
		r.AddError("custom guard %q: unknown guard type %q (use default, opt-in or a guard_types name)", name, cfg.Type)
	}
	if name == "all-default" || slices.ContainsFunc(builtinGuards, func(g seatbelt.Guard) bool { return g.Name() == name }) {
		r.AddError("custom guard %q conflicts with built-in guard", name)
	}
	if cfg.EnvOverride != "" && len(cfg.Paths) != 1 {
		r.AddError("custom guard %q: env_override requires exactly one path, got %d", name, len(cfg.Paths))
	}
	if len(cfg.Paths) == 0 {
		r.AddError("custom guard %q: at least one path is required", name)
	}
	return r
}

var (
	customMu      sync.RWMutex
	customGuards  []seatbelt.Guard
	customConfigs map[string]CustomGuardConfig
)

// SetCustomGuards validates defs and makes them the custom guard set,
// replacing any earlier set; nil clears it. An empty Type means
// "default". On error the earlier set stays in place.
func SetCustomGuards(defs map[string]CustomGuardConfig) error {
	var errs []error
	gs := make([]seatbelt.Guard, 0, len(defs))
	for _, name := range slices.Sorted(maps.Keys(defs)) {
		if err := ValidateCustomGuard(name, defs[name]).Err(); err != nil {
			errs = append(errs, err)
		}
		gs = append(gs, NewCustomGuard(name, defs[name]))
	}
	if err := errors.Join(errs...); err != nil {
		return err
	}
	customMu.Lock()
	defer customMu.Unlock()
	customGuards, customConfigs = gs, maps.Clone(defs)
	return nil
}

// CustomGuardConfigs returns the definitions of the current custom guard
// set, so a re-exec'd sandbox process can rebuild it.
func CustomGuardConfigs() map[string]CustomGuardConfig {
	customMu.RLock()
	defer customMu.RUnlock()
	return maps.Clone(customConfigs)
}
