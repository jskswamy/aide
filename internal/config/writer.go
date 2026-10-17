package config

import (
	"errors"
	"fmt"

	"github.com/jskswamy/aide/internal/fsutil"
	"gopkg.in/yaml.v3"
)

// ErrConfigNotLoaded reports a write attempted with the placeholder
// Config that stands in for a config that failed to load. Writing it
// would replace the user's file with an empty config.
var ErrConfigNotLoaded = errors.New("refusing to write config: it failed to load")

// FailedLoad returns the empty placeholder Config that best-effort
// callers use when Load fails. Reading it is safe; WriteConfig and
// WriteConfigTo refuse it, reporting loadErr.
func FailedLoad(loadErr error) *Config {
	if loadErr == nil {
		loadErr = errors.New("unknown error")
	}
	return &Config{loadErr: loadErr}
}

// WriteConfig writes the given Config to the global config file atomically.
func WriteConfig(cfg *Config) error {
	return WriteConfigTo(cfg, FilePath())
}

// WriteConfigTo writes the given Config to the given path atomically.
// This is the testable version that accepts an explicit path.
func WriteConfigTo(cfg *Config, path string) error {
	if cfg.loadErr != nil {
		return fmt.Errorf("%w (fix it first): %w", ErrConfigNotLoaded, cfg.loadErr)
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}
	home, _ := userHome()
	if err := validateLoadable(data, home, "refusing to write config: it"); err != nil {
		return err
	}
	return fsutil.AtomicWrite(path, data)
}
