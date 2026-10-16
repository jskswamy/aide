// Package config handles aide configuration loading, parsing, and normalization.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

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
	cfg, err := loadFile(globalPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// No global config file is not an error — return empty config
			return &Config{}, nil
		}
		return nil, fmt.Errorf("loading global config: %w", err)
	}

	// Normalize minimal format into full format (DD-12)
	if cfg.IsMinimal() && cfg.Agent != "" {
		cfg = normalizeMinimal(cfg)
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

// loadFile reads and unmarshals a single YAML config file.
func loadFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return &cfg, nil
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

// normalizeMinimal converts a flat (minimal) config into the full
// internal representation with a single "default" context.
func normalizeMinimal(cfg *Config) *Config {
	agentName := cfg.Agent
	if agentName == "" {
		agentName = "default"
	}
	// Minimal-format mcp_servers may be either a list of names (old
	// shape) or a v2 map; the MCPServerMap unmarshaller stores both
	// the same way (map with empty MCPServer values for the list form).
	var mcpList []string
	for k, v := range cfg.MCPServers {
		// Treat zero-value entries as legacy list-form names.
		if v.Command == "" && v.URL == "" {
			mcpList = append(mcpList, k)
		}
	}
	sort.Strings(mcpList)
	return &Config{
		Agents: map[string]AgentDef{
			agentName: {Binary: agentName},
		},
		Contexts: map[string]Context{
			"default": {
				Agent:      agentName,
				Env:        cfg.Env,
				Secret:     cfg.Secret,
				MCPServers: mcpList,
				Sandbox:    SandboxPolicyToRef(cfg.Sandbox),
				Yolo:       cfg.Yolo,
			},
		},
		NeverAllow:     cfg.NeverAllow,
		NeverAllowEnv:  cfg.NeverAllowEnv,
		ScrubEnv:       cfg.ScrubEnv,
		MCPServers:     cfg.MCPServers,
		DefaultContext: "default",
		Preferences:    cfg.Preferences,
		ProfileSchema:  cfg.ProfileSchema,
	}
}

// SandboxPolicyToRef wraps a SandboxPolicy pointer into a SandboxRef.
// Used when promoting a minimal config's or project override's sandbox field to a context.
func SandboxPolicyToRef(sp *SandboxPolicy) *SandboxRef {
	if sp == nil {
		return nil
	}
	if sp.Disabled {
		return &SandboxRef{Disabled: true}
	}
	return &SandboxRef{Inline: sp}
}
