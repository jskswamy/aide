package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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

// WriteConfig writes the given Config to the global config directory atomically.
func WriteConfig(cfg *Config) error {
	return WriteConfigTo(cfg, FilePath())
}

// WriteConfigTo writes the given Config using the modular split layout rooted
// at filepath.Dir(path), with path as the core config.yaml file.
// Unchanged files on disk are not rewritten (preserving mtime), and stale
// profile or emptied section files are removed.
func WriteConfigTo(cfg *Config, path string) error {
	if cfg.loadErr != nil {
		return fmt.Errorf("%w (fix it first): %w", ErrConfigNotLoaded, cfg.loadErr)
	}
	if cfg.LegacyMonolithic {
		return ErrLegacyMonolithicConfig
	}

	target := cfg
	if target.IsMinimal() && target.Agent != "" {
		target = normalizeMinimal(target)
		target.ProfileSchema = true
	}

	home, _ := userHome()
	if err := validateConfigStruct(target, home, "refusing to write config: it"); err != nil {
		return err
	}

	configDir := filepath.Dir(path)

	// 1. Marshal all files in memory first before touching disk.
	coreData, err := yaml.Marshal(target.extractCoreConfig())
	if err != nil {
		return fmt.Errorf("marshaling core config: %w", err)
	}

	sbx := target.extractSandboxFile()
	var sbxData []byte
	if !sbx.IsEmpty() {
		sbxData, err = yaml.Marshal(sbx)
		if err != nil {
			return fmt.Errorf("marshaling sandbox.yaml: %w", err)
		}
	}

	prov := target.extractProvisioningFile()
	var provData []byte
	if !prov.IsEmpty() {
		provData, err = yaml.Marshal(prov)
		if err != nil {
			return fmt.Errorf("marshaling provisioning.yaml: %w", err)
		}
	}

	profileBytes := make(map[string][]byte, len(target.Contexts))
	for _, name := range sortedKeys(target.Contexts) {
		if !gatewayIdentRE.MatchString(name) {
			return fmt.Errorf("%w: %q must match %s", ErrInvalidProfileFilename, name, gatewayIdentRE.String())
		}
		b, err := EncodeProfileYAML(target.Contexts[name])
		if err != nil {
			return fmt.Errorf("marshaling profiles/%s.yaml: %w", name, err)
		}
		profileBytes[name] = b
	}

	// 2. Write core config.yaml first (validates directory permissions before side effects).
	if err := writeIfChanged(path, coreData); err != nil {
		return err
	}

	// 3. Write or remove sandbox.yaml.
	sbxPath := filepath.Join(configDir, "sandbox.yaml")
	if len(sbxData) > 0 {
		if err := writeIfChanged(sbxPath, sbxData); err != nil {
			return err
		}
	} else if err := removeIfExists(sbxPath); err != nil {
		return err
	}

	// 4. Write or remove provisioning.yaml.
	provPath := filepath.Join(configDir, "provisioning.yaml")
	if len(provData) > 0 {
		if err := writeIfChanged(provPath, provData); err != nil {
			return err
		}
	} else if err := removeIfExists(provPath); err != nil {
		return err
	}

	// 5. Write active profiles/<name>.yaml and remove deleted ones.
	for _, name := range sortedKeys(target.Contexts) {
		pPath := ProfileFilePath(configDir, name)
		if err := writeIfChanged(pPath, profileBytes[name]); err != nil {
			return err
		}
	}
	for _, existing := range listProfileFiles(configDir) {
		name := strings.TrimSuffix(filepath.Base(existing), ".yaml")
		if _, active := profileBytes[name]; !active {
			if err := removeIfExists(existing); err != nil {
				return err
			}
		}
	}

	return nil
}

func validateConfigStruct(cfg *Config, home, what string) error {
	checks := []func() error{
		cfg.validateCredentials,
		cfg.ValidatePlugins,
		cfg.ValidateHooks,
	}
	if home != "" {
		checks = append(checks,
			func() error { return cfg.validateConfigDirs(home) },
			func() error { return cfg.validateProfileDirs(home) },
		)
	}
	for _, check := range checks {
		if err := check(); err != nil {
			return fmt.Errorf("%s would not load: %w", what, err)
		}
	}
	return nil
}

func writeIfChanged(path string, data []byte) error {
	if existing, err := os.ReadFile(path); err == nil && bytes.Equal(existing, data) {
		return nil
	}
	return fsutil.AtomicWrite(path, data)
}

func removeIfExists(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
