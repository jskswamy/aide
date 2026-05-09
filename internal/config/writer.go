package config

import (
	"fmt"

	"github.com/jskswamy/aide/internal/fsutil"
	"gopkg.in/yaml.v3"
)

// WriteConfig writes the given Config to the global config file atomically.
func WriteConfig(cfg *Config) error {
	return WriteConfigTo(cfg, FilePath())
}

// WriteConfigTo writes the given Config to the given path atomically.
// This is the testable version that accepts an explicit path.
func WriteConfigTo(cfg *Config, path string) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}
	return fsutil.AtomicWrite(path, data)
}
