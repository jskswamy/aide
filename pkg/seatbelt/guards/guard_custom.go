// Custom guard support for macOS Seatbelt profiles.
//
// NewCustomGuard builds a Guard from user-supplied configuration, allowing
// projects to protect arbitrary paths without writing Go code.

package guards

import (
	"fmt"
	"path/filepath"

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
func (g *customGuard) Type() string        { return g.cfg.Type }
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
					defaultPath = ExpandTilde(g.cfg.Paths[0], ctx.HomeDir)
				}
				result.Overrides = append(result.Overrides, seatbelt.Override{
					EnvVar:      g.cfg.EnvOverride,
					Value:       v,
					DefaultPath: defaultPath,
				})
			}
		}
	}

	paths := g.resolvePaths(ctx)

	// Deny rules for each path, with existence check.
	for _, p := range paths {
		if pathExists(p) {
			result.Rules = append(result.Rules, DenyDir(p)...)
			result.Protected = append(result.Protected, p)
		} else {
			result.Skipped = append(result.Skipped, fmt.Sprintf("%s not found", p))
		}
	}

	// Allow rules for explicitly allowed paths.
	for _, a := range g.cfg.Allowed {
		expanded := filepath.Clean(ExpandTilde(a, ctx.HomeDir))
		result.Rules = append(result.Rules, AllowReadFile(expanded))
		result.Allowed = append(result.Allowed, expanded)
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
		out = append(out, ExpandTilde(p, ctx.HomeDir))
	}
	return out
}

// ValidateCustomGuard checks the configuration for common mistakes.
// Returns a ValidationResult; check result.OK() to determine if validation passed.
func ValidateCustomGuard(name string, cfg CustomGuardConfig) *seatbelt.ValidationResult {
	r := &seatbelt.ValidationResult{}
	if cfg.Type == "always" {
		r.AddError("custom guard %q cannot use type \"always\"", name)
	}
	if _, ok := GuardByName(name); ok {
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
