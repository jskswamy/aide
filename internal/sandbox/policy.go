package sandbox

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/jskswamy/aide/internal/config"
)

// PolicyFromConfig builds a sandbox.Policy from a SandboxPolicy config and
// the default policy. User-specified fields override defaults;
// unspecified fields use defaults.
//
// Merge rules:
//   - writable/readable/denied: if user specifies any entries, they REPLACE
//     the default list (not append). This gives full control.
//   - network/allow_subprocess/clean_env: if user specifies, overrides default.
//   - sandbox: false (Disabled=true) -> returns nil (no sandbox).
//   - sandbox: absent (cfg==nil) -> returns DefaultPolicy unchanged.
func PolicyFromConfig(
	cfg *config.SandboxPolicy,
	projectRoot, runtimeDir, homeDir, tempDir string,
) (*Policy, error) {
	if cfg != nil && cfg.Disabled {
		return nil, nil // nil policy = no sandbox
	}

	defaults := DefaultPolicy(projectRoot, runtimeDir, homeDir, tempDir)

	if cfg == nil {
		return &defaults, nil
	}

	templateVars := map[string]string{
		"project_root": projectRoot,
		"runtime_dir":  runtimeDir,
		"home":         homeDir,
		"config_dir":   filepath.Join(homeDir, ".config", "aide"),
	}

	policy := defaults // copy

	if len(cfg.Writable) > 0 {
		w, err := ResolvePaths(cfg.Writable, templateVars)
		if err != nil {
			return nil, err
		}
		policy.Writable = w
	}

	if len(cfg.Readable) > 0 {
		r, err := ResolvePaths(cfg.Readable, templateVars)
		if err != nil {
			return nil, err
		}
		policy.Readable = r
	}

	if len(cfg.Denied) > 0 {
		d, err := ResolvePaths(cfg.Denied, templateVars)
		if err != nil {
			return nil, err
		}
		policy.Denied = d
	}

	if cfg.Network != "" {
		policy.Network = NetworkMode(cfg.Network)
	}

	if cfg.AllowSubprocess != nil {
		policy.AllowSubprocess = *cfg.AllowSubprocess
	}

	if cfg.CleanEnv != nil {
		policy.CleanEnv = *cfg.CleanEnv
	}

	return &policy, nil
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

// ValidateSandboxConfig validates a SandboxPolicy configuration.
func ValidateSandboxConfig(cfg *config.SandboxPolicy) error {
	if cfg == nil || cfg.Disabled {
		return nil
	}
	validNetworkModes := map[string]bool{
		"outbound": true, "none": true, "unrestricted": true, "": true,
	}
	if !validNetworkModes[cfg.Network] {
		return fmt.Errorf(
			"sandbox.network: invalid value %q, must be one of: outbound, none, unrestricted",
			cfg.Network,
		)
	}
	return nil
}
