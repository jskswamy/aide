package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// WriteConfig writes the given Config to the global config file atomically.
// It writes to a .tmp file first, then renames.
func WriteConfig(cfg *Config) error {
	return WriteConfigTo(cfg, ConfigFilePath())
}

// WriteConfigTo writes the given Config to the given path atomically.
// This is the testable version that accepts an explicit path.
func WriteConfigTo(cfg *Config, path string) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("writing temp config file: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		// Clean up .tmp on failure
		os.Remove(tmpPath)
		return fmt.Errorf("renaming temp config file: %w", err)
	}

	return nil
}
