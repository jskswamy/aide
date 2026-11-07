// Package config handles aide configuration loading, parsing, and normalization.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Load reads the global config from configDir/config.yaml and optionally
// discovers a project-level .aide.yaml by walking up from projectDir to
// the git root (or filesystem root).
//
// configDir: typically Dir() ($XDG_CONFIG_HOME/aide/)
// projectDir: typically the current working directory or a subdirectory
//
// If the global config file does not exist, an empty Config is returned
// with no error. If it exists but contains invalid YAML, an error is returned.
//
// The returned Config has its ProjectOverride populated if a .aide.yaml
// was found during the directory walk.
func Load(configDir, projectDir string) (*Config, error) {
	globalPath := filepath.Join(configDir, "config.yaml")
	var (
		cfg           *Config
		rawBytes      []byte
		configExisted bool
	)
	data, err := os.ReadFile(globalPath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("loading global config: %w", err)
		}
		if !HasAnyConfigFile(configDir) {
			// No global config file and no split files — return empty config
			return &Config{}, nil
		}
		cfg = &Config{ProfileSchema: true}
	} else {
		configExisted = true
		rawBytes = data
		var parsed Config
		if err := yaml.Unmarshal(data, &parsed); err != nil {
			return nil, fmt.Errorf("loading global config: parsing %s: %w", globalPath, err)
		}
		cfg = &parsed
	}

	if err := loadSplitFiles(configDir, cfg, configExisted, rawBytes); err != nil {
		return nil, fmt.Errorf("loading global config: %w", err)
	}

	if err := cfg.loadProjectConfig(projectDir); err != nil {
		return nil, err
	}

	if home, ok := userHome(); ok {
		if err := cfg.validateConfigDirs(home); err != nil {
			return nil, fmt.Errorf("config: %w", err)
		}
		if err := cfg.validateProfileDirs(home); err != nil {
			return nil, fmt.Errorf("config: %w", err)
		}
	}

	if err := cfg.validateCredentials(); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	if err := cfg.ValidatePlugins(); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	if err := cfg.ValidateHooks(); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}

	return cfg, nil
}

// LoadProjectOverrideFile reads one .aide.yaml as written, with no
// path rewriting or layering. Writers edit this form. A missing file
// is an empty override.
func LoadProjectOverrideFile(path string) (*ProjectOverride, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- the user's own .aide.yaml
	if errors.Is(err, os.ErrNotExist) {
		return &ProjectOverride{}, nil
	}
	if err != nil {
		return nil, err
	}
	return parseProjectOverride(path, data)
}

func parseProjectOverride(path string, data []byte) (*ProjectOverride, error) {
	var override ProjectOverride
	if err := yaml.Unmarshal(data, &override); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return &override, nil
}

// loadProjectConfig discovers and loads the .aide.yaml layers that
// apply in projectDir and merges them into ProjectOverride. Every
// layer is checked structurally (it may name a profile, never define
// one) before any trust decision.
func (c *Config) loadProjectConfig(projectDir string) error {
	d := DiscoverProject(projectDir)
	c.ProjectConfigErr = d.Err
	if d.Warning != "" {
		c.ProjectConfigWarnings = append(c.ProjectConfigWarnings, d.Warning)
	}
	for _, l := range []struct{ source, path string }{
		{LayerInherited, d.MainPath},
		{LayerLocal, d.LocalPath},
	} {
		if l.path == "" {
			continue
		}
		data, err := os.ReadFile(l.path) // #nosec G304 -- discovered .aide.yaml
		if err != nil && l.source == LayerInherited {
			c.ProjectConfigErr = mainUnavailable(d.CheckoutRoot, l.path, err)
			continue
		}
		if err != nil {
			return fmt.Errorf("loading project config %s: %w", l.path, err)
		}
		po, err := parseProjectOverride(l.path, data)
		if err != nil {
			return fmt.Errorf("loading project config %s: %w", l.path, err)
		}
		if hasRelativePaths(po) {
			root, err := anchorRoot(l.path)
			if err != nil {
				c.ProjectConfigWarnings = append(c.ProjectConfigWarnings, fmt.Sprintf(
					"%s not applied: it has relative paths and the checkout holding it cannot be determined (%v); "+
						"use absolute or ~/ paths there", l.path, err))
				continue
			}
			rewriteRelativePaths(po, root)
		}
		c.ProjectLayers = append(c.ProjectLayers, ProjectLayer{Source: l.source, Path: l.path, Contents: data, Override: po})
		c.ProjectConfigPath = l.path
	}
	c.ProjectOverride = MergeProjectLayers(c.ProjectLayers)
	return nil
}

// SandboxPolicyToRef wraps a SandboxPolicy pointer into a SandboxRef.
// Used when promoting a project override's sandbox field to a context.
func SandboxPolicyToRef(sp *SandboxPolicy) *SandboxRef {
	if sp == nil {
		return nil
	}
	if sp.Disabled {
		return &SandboxRef{Disabled: true}
	}
	return &SandboxRef{Inline: sp}
}
