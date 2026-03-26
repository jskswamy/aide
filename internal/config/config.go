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

	// Walk up from projectDir looking for .aide.yaml
	projectConfigPath := findProjectConfig(projectDir)
	if projectConfigPath != "" {
		override, err := loadProjectOverride(projectConfigPath)
		if err != nil {
			return nil, fmt.Errorf("loading project config %s: %w", projectConfigPath, err)
		}
		cfg.ProjectOverride = override
		cfg.ProjectConfigPath = projectConfigPath
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

// loadProjectOverride reads a .aide.yaml file and returns a ProjectOverride.
func loadProjectOverride(path string) (*ProjectOverride, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var override ProjectOverride
	if err := yaml.Unmarshal(data, &override); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return &override, nil
}

// normalizeMinimal converts a flat (minimal) config into the full
// internal representation with a single "default" context.
func normalizeMinimal(cfg *Config) *Config {
	agentName := cfg.Agent
	if agentName == "" {
		agentName = "default"
	}
	return &Config{
		Agents: map[string]AgentDef{
			agentName: {Binary: agentName},
		},
		Contexts: map[string]Context{
			"default": {
				Agent:       agentName,
				Env:         cfg.Env,
				Secret:      cfg.Secret,
				MCPServers:  cfg.MCPServers,
				Sandbox:     SandboxPolicyToRef(cfg.Sandbox),
				Yolo:        cfg.Yolo,
			},
		},
		MCP:            cfg.MCP,
		DefaultContext: "default",
		Preferences:    cfg.Preferences,
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

// findProjectConfig walks up from startDir looking for .aide.yaml.
// It stops at the git root (directory containing .git) or the filesystem root.
// Returns the path to .aide.yaml if found, or empty string if not found.
func findProjectConfig(startDir string) string {
	dir := startDir
	for {
		candidate := filepath.Join(dir, ProjectConfigFileName)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}

		// Check if we've reached a git root — if so, stop after checking this dir
		gitDir := filepath.Join(dir, ".git")
		if _, err := os.Stat(gitDir); err == nil {
			// We're at the git root and didn't find .aide.yaml here (already checked above)
			// Actually we did check this dir already, so stop
			return ""
		}

		// Move to parent directory
		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached filesystem root
			return ""
		}
		dir = parent
	}
}
