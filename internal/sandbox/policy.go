package sandbox

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/pkg/seatbelt"
	"github.com/jskswamy/aide/pkg/seatbelt/guards"
)

// PolicyFromConfig builds a sandbox.Policy from a SandboxPolicy config and
// the default policy. User-specified fields override defaults;
// unspecified fields use defaults.
//
// Guard resolution logic:
//   - No guards/guards_extra -> use default (always + default)
//   - guards: set -> always guards + listed guards (replaces default set)
//   - guards_extra: set -> default + extra
//   - Both set -> warn, ignore guards_extra
//   - Expand meta-guards (cloud -> 5 providers)
//   - Unguard: remove listed guards. Cannot unguard "always" type -> error
//   - Unknown guard name -> error
//
// Callers must resolve SandboxRef to SandboxPolicy before calling this function.
// A disabled sandbox (sandbox: false) should not reach this function -- the
// caller should skip sandbox entirely.
func PolicyFromConfig(
	cfg *config.SandboxPolicy,
	projectRoot, runtimeDir, homeDir, tempDir string,
) (*Policy, []string, error) {
	defaults := DefaultPolicy(projectRoot, runtimeDir, tempDir, nil)

	if cfg == nil {
		return &defaults, nil, nil
	}

	var warnings []string

	templateVars := map[string]string{
		"project_root": projectRoot,
		"runtime_dir":  runtimeDir,
		"home":         homeDir,
		"config_dir":   filepath.Join(homeDir, ".config", "aide"),
	}

	policy := defaults // copy

	// --- Guard resolution ---
	resolvedGuards, guardWarnings, err := resolveGuards(cfg)
	if err != nil {
		return nil, nil, err
	}
	warnings = append(warnings, guardWarnings...)
	if resolvedGuards != nil {
		policy.Guards = resolvedGuards
	}

	// --- ExtraDenied from denied/denied_extra ---
	if len(cfg.Denied) > 0 {
		d, err := ResolvePaths(cfg.Denied, templateVars)
		if err != nil {
			return nil, nil, err
		}
		policy.ExtraDenied = validateAndFilterPaths(d, &warnings)
	} else if len(cfg.DeniedExtra) > 0 {
		extra, err := ResolvePaths(cfg.DeniedExtra, templateVars)
		if err != nil {
			return nil, nil, err
		}
		policy.ExtraDenied = append(policy.ExtraDenied, validateAndFilterPaths(extra, &warnings)...)
	}

	// --- ExtraWritable from writable/writable_extra ---
	if len(cfg.Writable) > 0 {
		w, err := ResolvePaths(cfg.Writable, templateVars)
		if err != nil {
			return nil, nil, err
		}
		policy.ExtraWritable = validateAndFilterPaths(w, &warnings)
	} else if len(cfg.WritableExtra) > 0 {
		extra, err := ResolvePaths(cfg.WritableExtra, templateVars)
		if err != nil {
			return nil, nil, err
		}
		policy.ExtraWritable = validateAndFilterPaths(extra, &warnings)
	}

	// --- ExtraReadable from readable/readable_extra ---
	if len(cfg.Readable) > 0 {
		r, err := ResolvePaths(cfg.Readable, templateVars)
		if err != nil {
			return nil, nil, err
		}
		policy.ExtraReadable = validateAndFilterPaths(r, &warnings)
	} else if len(cfg.ReadableExtra) > 0 {
		extra, err := ResolvePaths(cfg.ReadableExtra, templateVars)
		if err != nil {
			return nil, nil, err
		}
		policy.ExtraReadable = validateAndFilterPaths(extra, &warnings)
	}

	if cfg.Network != nil && cfg.Network.Mode != "" {
		policy.Network = NetworkMode(cfg.Network.Mode)
	}

	// Extract port filtering from NetworkPolicy
	if cfg.Network != nil {
		policy.AllowPorts = cfg.Network.AllowPorts
		policy.DenyPorts = cfg.Network.DenyPorts
	}

	if cfg.AllowSubprocess != nil {
		policy.AllowSubprocess = *cfg.AllowSubprocess
	}

	if cfg.CleanEnv != nil {
		policy.CleanEnv = *cfg.CleanEnv
	}

	return &policy, warnings, nil
}

// resolveGuards resolves guard names from config fields.
// Returns (nil, warnings, nil) if no guard config is specified (use defaults).
func resolveGuards(cfg *config.SandboxPolicy) ([]string, []string, error) {
	hasGuards := len(cfg.Guards) > 0
	hasGuardsExtra := len(cfg.GuardsExtra) > 0
	hasUnguard := len(cfg.Unguard) > 0

	if !hasGuards && !hasGuardsExtra && !hasUnguard {
		return nil, nil, nil // use defaults
	}

	var warnings []string
	var guardNames []string

	if hasGuards && hasGuardsExtra {
		warnings = append(warnings,
			"sandbox: both guards and guards_extra are set; guards_extra is ignored when guards is specified",
		)
	}

	switch {
	case hasGuards:
		// Always guards + listed guards (replaces default set)
		alwaysNames := alwaysGuardNames()
		guardNames = append(guardNames, alwaysNames...)
		for _, name := range cfg.Guards {
			expanded := guards.ExpandGuardName(name)
			for _, n := range expanded {
				if !containsString(alwaysNames, n) {
					guardNames = append(guardNames, n)
				}
			}
		}
	case hasGuardsExtra:
		// Default + extra
		guardNames = append(guardNames, guards.DefaultGuardNames()...)
		for _, name := range cfg.GuardsExtra {
			expanded := guards.ExpandGuardName(name)
			guardNames = append(guardNames, expanded...)
		}
	default:
		// Only unguard specified -- start from defaults
		guardNames = append(guardNames, guards.DefaultGuardNames()...)
	}

	// Validate all guard names exist
	for _, name := range guardNames {
		if _, ok := guards.GuardByName(name); !ok {
			return nil, nil, fmt.Errorf("sandbox: unknown guard name %q", name)
		}
	}

	// Apply unguard
	if hasUnguard {
		for _, name := range cfg.Unguard {
			expanded := guards.ExpandGuardName(name)
			for _, n := range expanded {
				g, ok := guards.GuardByName(n)
				if !ok {
					return nil, nil, fmt.Errorf("sandbox: unknown guard name %q in unguard", n)
				}
				if g.Type() == "always" {
					return nil, nil, fmt.Errorf("sandbox: cannot unguard %q (type %q is always-on)", n, g.Type())
				}
				guardNames = removeString(guardNames, n)
			}
		}
	}

	// Deduplicate while preserving order
	guardNames = dedup(guardNames)

	return guardNames, warnings, nil
}

// alwaysGuardNames returns names of all "always" type guards.
func alwaysGuardNames() []string {
	var names []string
	for _, g := range guards.AllGuards() {
		if g.Type() == "always" {
			names = append(names, g.Name())
		}
	}
	return names
}

// containsString checks if a string slice contains a value.
func containsString(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

// removeString removes all occurrences of s from slice.
func removeString(slice []string, s string) []string {
	var result []string
	for _, v := range slice {
		if v != s {
			result = append(result, v)
		}
	}
	return result
}

// dedup removes duplicates while preserving order.
func dedup(slice []string) []string {
	seen := make(map[string]bool, len(slice))
	var result []string
	for _, s := range slice {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	return result
}

// isGlobPattern reports whether path contains any glob metacharacters.
func isGlobPattern(path string) bool {
	return strings.ContainsAny(path, "*?[{")
}

// validateAndFilterPaths checks each resolved path. Non-glob paths that
// don't exist on disk are skipped and a warning is added.
func validateAndFilterPaths(paths []string, warnings *[]string) []string {
	var filtered []string
	for _, p := range paths {
		if isGlobPattern(p) {
			filtered = append(filtered, p)
			continue
		}
		if _, err := os.Lstat(p); err != nil {
			*warnings = append(*warnings, fmt.Sprintf("skipped: %s (not found)", p))
			continue
		}
		filtered = append(filtered, p)
	}
	return filtered
}

// ResolvePaths resolves template variables and ~ in a list of path strings.
func ResolvePaths(paths []string, vars map[string]string) ([]string, error) {
	var resolved []string
	for _, p := range paths {
		// 1. Resolve {{ .var }} templates
		r, err := resolvePathTemplate(p, vars)
		if err != nil {
			return nil, fmt.Errorf("resolving path %q: %w", p, err)
		}
		// 2. Expand ~
		if strings.HasPrefix(r, "~/") {
			r = filepath.Join(vars["home"], r[2:])
		}
		resolved = append(resolved, r)
	}
	return resolved, nil
}

// resolvePathTemplate resolves a single path template string using the given variables.
func resolvePathTemplate(tmplStr string, vars map[string]string) (string, error) {
	tmpl, err := template.New("path").Option("missingkey=error").Parse(tmplStr)
	if err != nil {
		return "", fmt.Errorf("template parse error: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, vars); err != nil {
		return "", fmt.Errorf("template execution error: %w", err)
	}

	return buf.String(), nil
}

// ValidateSandboxConfigDetailed validates a SandboxPolicy configuration,
// returning both errors and warnings.
//
// Note: writable, readable, writable_extra, and readable_extra fields on
// SandboxPolicy are retained for backward compatibility with existing configs
// that predate the guard system. They are validated here but are not used in
// guard-based profile generation (guards handle path access control instead).
func ValidateSandboxConfigDetailed(cfg *config.SandboxPolicy) *seatbelt.ValidationResult {
	result := &seatbelt.ValidationResult{}
	if cfg == nil {
		return result
	}

	// Validate network mode
	if cfg.Network != nil {
		validNetworkModes := map[string]bool{
			"outbound": true, "none": true, "unrestricted": true, "": true,
		}
		if !validNetworkModes[cfg.Network.Mode] {
			result.AddError("sandbox.network: invalid value %q, must be one of: outbound, none, unrestricted",
				cfg.Network.Mode)
		}

		// Validate port numbers in AllowPorts
		for _, port := range cfg.Network.AllowPorts {
			if port < 1 || port > 65535 {
				result.AddError("sandbox.network.allow_ports: invalid port %d, must be 1-65535", port)
			}
		}

		// Validate port numbers in DenyPorts
		for _, port := range cfg.Network.DenyPorts {
			if port < 1 || port > 65535 {
				result.AddError("sandbox.network.deny_ports: invalid port %d, must be 1-65535", port)
			}
		}
	}

	// Warn if both denied and denied_extra are set (extra is ignored when override is present)
	if len(cfg.Denied) > 0 && len(cfg.DeniedExtra) > 0 {
		result.AddWarning("sandbox: both denied and denied_extra are set; denied_extra is ignored when denied is specified")
	}

	// Warn if both writable and writable_extra are set
	if len(cfg.Writable) > 0 && len(cfg.WritableExtra) > 0 {
		result.AddWarning("sandbox: both writable and writable_extra are set; writable_extra is ignored when writable is specified")
	}

	// Warn if both readable and readable_extra are set
	if len(cfg.Readable) > 0 && len(cfg.ReadableExtra) > 0 {
		result.AddWarning("sandbox: both readable and readable_extra are set; readable_extra is ignored when readable is specified")
	}

	// Warn if both guards and guards_extra are set
	if len(cfg.Guards) > 0 && len(cfg.GuardsExtra) > 0 {
		result.AddWarning("sandbox: both guards and guards_extra are set; guards_extra is ignored when guards is specified")
	}

	// Warn if writable contains home dir (too broad)
	for _, w := range cfg.Writable {
		if w == "~" || w == "~/" || isHomeDirPath(w) {
			result.AddWarning("sandbox.writable: %q includes the entire home directory, which is very broad", w)
		}
	}

	// Validate guard names
	for _, name := range cfg.Guards {
		expanded := guards.ExpandGuardName(name)
		for _, n := range expanded {
			if _, ok := guards.GuardByName(n); !ok {
				result.AddError("sandbox.guards: unknown guard name %q", n)
			}
		}
	}

	for _, name := range cfg.GuardsExtra {
		expanded := guards.ExpandGuardName(name)
		for _, n := range expanded {
			if _, ok := guards.GuardByName(n); !ok {
				result.AddError("sandbox.guards_extra: unknown guard name %q", n)
			}
		}
	}

	// Validate unguard names and type restriction
	for _, name := range cfg.Unguard {
		expanded := guards.ExpandGuardName(name)
		for _, n := range expanded {
			g, ok := guards.GuardByName(n)
			if !ok {
				result.AddError("sandbox.unguard: unknown guard name %q", n)
				continue
			}
			if g.Type() == "always" {
				result.AddError("sandbox.unguard: cannot unguard %q (type %q is always-on)", n, g.Type())
			}
		}
	}

	return result
}

// isHomeDirPath returns true if the path looks like a user home directory.
func isHomeDirPath(path string) bool {
	// Match common home directory patterns: /home/*, /Users/*
	if strings.HasPrefix(path, "/home/") || strings.HasPrefix(path, "/Users/") {
		// Must be exactly /home/user or /Users/user (no further subdirectory)
		parts := strings.Split(strings.TrimRight(path, "/"), "/")
		return len(parts) == 3
	}
	return false
}

// ValidateSandboxConfig validates a SandboxPolicy configuration.
// Returns the first error found, or nil. For detailed results including
// warnings, use ValidateSandboxConfigDetailed.
func ValidateSandboxConfig(cfg *config.SandboxPolicy) error {
	return ValidateSandboxConfigDetailed(cfg).Err()
}

// ValidateSandboxRef validates a SandboxRef, checking that profile references
// resolve to entries in the sandboxes map.
func ValidateSandboxRef(ref *config.SandboxRef, sandboxes map[string]config.SandboxPolicy) error {
	if ref == nil || ref.Disabled {
		return nil
	}
	if ref.Inline != nil {
		return ValidateSandboxConfig(ref.Inline)
	}
	if ref.ProfileName != "" {
		if ref.ProfileName == "default" || ref.ProfileName == "none" {
			return nil
		}
		if sandboxes == nil {
			return fmt.Errorf("sandbox profile %q not found (no sandboxes defined)", ref.ProfileName)
		}
		if _, ok := sandboxes[ref.ProfileName]; !ok {
			return fmt.Errorf("sandbox profile %q not found in sandboxes map", ref.ProfileName)
		}
		sp := sandboxes[ref.ProfileName]
		return ValidateSandboxConfig(&sp)
	}
	return nil
}

// ResolveSandboxRef resolves a SandboxRef into a *SandboxPolicy suitable for
// passing to PolicyFromConfig. Returns:
//   - (nil, false, nil) when ref is nil -> use default policy
//   - (nil, true, nil) when sandbox is disabled -> skip sandbox entirely
//   - (*SandboxPolicy, false, nil) when resolved to an inline or named profile
//   - (nil, false, err) on error (e.g. unknown profile name)
func ResolveSandboxRef(ref *config.SandboxRef, sandboxes map[string]config.SandboxPolicy) (*config.SandboxPolicy, bool, error) {
	if ref == nil {
		return nil, false, nil
	}
	if ref.Disabled {
		return nil, true, nil
	}
	if ref.Inline != nil {
		return ref.Inline, false, nil
	}
	if ref.ProfileName == "" || ref.ProfileName == "default" {
		return nil, false, nil
	}
	if ref.ProfileName == "none" {
		return nil, true, nil
	}
	if sandboxes == nil {
		return nil, false, fmt.Errorf("sandbox profile %q not found (no sandboxes defined)", ref.ProfileName)
	}
	sp, ok := sandboxes[ref.ProfileName]
	if !ok {
		return nil, false, fmt.Errorf("sandbox profile %q not found in sandboxes map", ref.ProfileName)
	}
	return &sp, false, nil
}
