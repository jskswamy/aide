// Custom guard support for macOS Seatbelt profiles.
//
// NewCustomGuard builds a Guard from user-supplied configuration, allowing
// projects to protect arbitrary paths without writing Go code.

package guards

import (
	"path/filepath"
	"strings"

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

func (g *customGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	paths := g.resolvePaths(ctx)

	var rules []seatbelt.Rule
	// Deny rules for each path.
	for _, p := range paths {
		rules = append(rules,
			seatbelt.Raw(`(deny file-read-data
    `+`(subpath "`+p+`")`+`
)`),
			seatbelt.Raw(`(deny file-write*
    `+`(subpath "`+p+`")`+`
)`),
		)
	}

	// Allow rules for explicitly allowed paths.
	for _, a := range g.cfg.Allowed {
		expanded := expandHome(ctx, a)
		rules = append(rules,
			seatbelt.Raw(`(allow file-read*
    `+`(literal "`+filepath.Clean(expanded)+`")`+`
)`),
		)
	}

	return rules
}

// resolvePaths returns the effective list of absolute paths for the guard,
// applying home-dir expansion and optional env-override.
func (g *customGuard) resolvePaths(ctx *seatbelt.Context) []string {
	// If EnvOverride is set and the env var resolves to a single path, use it.
	if g.cfg.EnvOverride != "" {
		if v, ok := ctx.EnvLookup(g.cfg.EnvOverride); ok && v != "" {
			parts := splitColonPaths(v)
			if len(parts) == 1 {
				return parts
			}
		}
	}

	var out []string
	for _, p := range g.cfg.Paths {
		out = append(out, expandHome(ctx, p))
	}
	return out
}

// expandHome replaces a leading "~/" with ctx.HomeDir.
func expandHome(ctx *seatbelt.Context, p string) string {
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(ctx.HomeDir, p[2:])
	}
	return p
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
