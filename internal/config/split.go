package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

var (
	// ErrDuplicateConfigSection reports that a config section (sandbox or
	// provisioning) is defined in both config.yaml and its dedicated split file.
	ErrDuplicateConfigSection = errors.New("config section defined in multiple files")

	// ErrDuplicateProfile reports that a profile is defined both inline in
	// config.yaml and in profiles/<name>.yaml.
	ErrDuplicateProfile = errors.New("profile defined in multiple files")

	// ErrLegacyMonolithicConfig reports an attempt to write a config that was
	// loaded from a legacy single-file config.yaml before migration.
	ErrLegacyMonolithicConfig = errors.New("refusing to write config: legacy single-file config must be migrated first (run 'aide config migrate --write')")

	// ErrInvalidProfileFilename reports a profile name that is not a valid
	// identifier for profiles/<name>.yaml.
	ErrInvalidProfileFilename = errors.New("invalid profile filename")
)

// SandboxFile holds the security, guard, and capability fields stored in
// ~/.config/aide/sandbox.yaml.
type SandboxFile struct {
	Capabilities  map[string]CapabilityDef `yaml:"capabilities,omitempty"`
	Sandboxes     map[string]SandboxPolicy `yaml:"sandboxes,omitempty"`
	CustomGuards  map[string]CustomGuard   `yaml:"custom_guards,omitempty"`
	GuardTypes    map[string]GuardType     `yaml:"guard_types,omitempty"`
	NeverAllow    []string                 `yaml:"never_allow,omitempty"`
	NeverAllowEnv []string                 `yaml:"never_allow_env,omitempty"`
	ScrubEnv      []string                 `yaml:"scrub_env,omitempty"`
}

var validSandboxFileKeys = []string{
	"capabilities", "sandboxes", "custom_guards", "guard_types",
	"never_allow", "never_allow_env", "scrub_env",
}

// IsEmpty reports whether s has no sandbox settings populated.
func (s SandboxFile) IsEmpty() bool {
	return len(s.Capabilities) == 0 &&
		len(s.Sandboxes) == 0 &&
		len(s.CustomGuards) == 0 &&
		len(s.GuardTypes) == 0 &&
		len(s.NeverAllow) == 0 &&
		len(s.NeverAllowEnv) == 0 &&
		len(s.ScrubEnv) == 0
}

// ProvisioningFile holds the shared tooling declarations stored in
// ~/.config/aide/provisioning.yaml.
type ProvisioningFile struct {
	Plugins    PluginMap    `yaml:"plugins,omitempty"`
	MCPServers MCPServerMap `yaml:"mcp_servers,omitempty"`
	Hooks      HooksMap     `yaml:"hooks,omitempty"`
}

var validProvisioningFileKeys = []string{
	"plugins", "mcp_servers", "hooks",
}

// IsEmpty reports whether p has no provisioning settings populated.
func (p ProvisioningFile) IsEmpty() bool {
	return len(p.Plugins) == 0 &&
		len(p.MCPServers) == 0 &&
		len(p.Hooks) == 0
}

// CoreConfig holds the global settings that remain in ~/.config/aide/config.yaml
// under the split layout.
type CoreConfig struct {
	DefaultProfile string            `yaml:"default_profile,omitempty"`
	Agents         map[string]AgentDef `yaml:"agents,omitempty"`
	Preferences    *Preferences      `yaml:"preferences,omitempty"`
	Statusline     *StatuslineConfig `yaml:"statusline,omitempty"`
}

func (c *Config) hasInlineSandbox() bool {
	return !c.extractSandboxFile().IsEmpty()
}

func (c *Config) hasInlineProvisioning() bool {
	return !c.extractProvisioningFile().IsEmpty()
}

func (c *Config) hasInlineProfiles() bool {
	return len(c.Contexts) > 0
}

func (c *Config) extractSandboxFile() SandboxFile {
	return SandboxFile{
		Capabilities:  c.Capabilities,
		Sandboxes:     c.Sandboxes,
		CustomGuards:  c.CustomGuards,
		GuardTypes:    c.GuardTypes,
		NeverAllow:    c.NeverAllow,
		NeverAllowEnv: c.NeverAllowEnv,
		ScrubEnv:      c.ScrubEnv,
	}
}

func (c *Config) extractProvisioningFile() ProvisioningFile {
	return ProvisioningFile{
		Plugins:    c.Plugins,
		MCPServers: c.MCPServers,
		Hooks:      c.Hooks,
	}
}

func (c *Config) extractCoreConfig() CoreConfig {
	defaultProfile := c.DefaultContext
	if c.SynthesizedDefaultContext && defaultProfile == DefaultProfileName {
		defaultProfile = ""
	}
	return CoreConfig{
		DefaultProfile: defaultProfile,
		Agents:         c.Agents,
		Preferences:    c.Preferences,
		Statusline:     c.Statusline,
	}
}

// loadSplitFiles loads sandbox.yaml, provisioning.yaml, and profiles/*.yaml
// from configDir and merges them into cfg, checking for duplicate definitions
// against what config.yaml already declared.
func loadSplitFiles(configDir string, cfg *Config, configExisted bool, rawBytes []byte) error {
	var rootKeys []string
	if configExisted {
		var doc yaml.Node
		if err := yaml.Unmarshal(rawBytes, &doc); err == nil {
			rootKeys = mappingKeys(&doc)
		}
	}
	usedLegacyContextKeys := slices.Contains(rootKeys, keyContexts) || slices.Contains(rootKeys, keyDefaultContext)
	if configExisted {
		if !usedLegacyContextKeys {
			cfg.ProfileSchema = true
		}
		if usedLegacyContextKeys || cfg.hasInlineProfiles() || cfg.hasInlineSandbox() || cfg.hasInlineProvisioning() {
			cfg.LegacyMonolithic = true
		}
	} else {
		cfg.ProfileSchema = true
	}

	// 1. Load sandbox.yaml if present
	sbxPath := filepath.Join(configDir, "sandbox.yaml")
	if data, err := os.ReadFile(sbxPath); err == nil {
		if cfg.hasInlineSandbox() {
			return fmt.Errorf("%w: sandbox settings are defined in both config.yaml and sandbox.yaml; run 'aide config migrate --write' to consolidate", ErrDuplicateConfigSection)
		}
		var node yaml.Node
		if err := yaml.Unmarshal(data, &node); err != nil {
			return fmt.Errorf("parsing %s: %w", sbxPath, err)
		}
		if err := checkAllowedKeys(&node, validSandboxFileKeys, sbxPath); err != nil {
			return err
		}
		var sbx SandboxFile
		if err := node.Decode(&sbx); err != nil {
			return fmt.Errorf("parsing %s: %w", sbxPath, err)
		}
		cfg.Capabilities = sbx.Capabilities
		cfg.Sandboxes = sbx.Sandboxes
		cfg.CustomGuards = sbx.CustomGuards
		cfg.GuardTypes = sbx.GuardTypes
		cfg.NeverAllow = sbx.NeverAllow
		cfg.NeverAllowEnv = sbx.NeverAllowEnv
		cfg.ScrubEnv = sbx.ScrubEnv
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("reading %s: %w", sbxPath, err)
	}

	// 2. Load provisioning.yaml if present
	provPath := filepath.Join(configDir, "provisioning.yaml")
	if data, err := os.ReadFile(provPath); err == nil {
		if cfg.hasInlineProvisioning() {
			return fmt.Errorf("%w: provisioning settings are defined in both config.yaml and provisioning.yaml; run 'aide config migrate --write' to consolidate", ErrDuplicateConfigSection)
		}
		var node yaml.Node
		if err := yaml.Unmarshal(data, &node); err != nil {
			return fmt.Errorf("parsing %s: %w", provPath, err)
		}
		if err := checkAllowedKeys(&node, validProvisioningFileKeys, provPath); err != nil {
			return err
		}
		var prov ProvisioningFile
		if err := node.Decode(&prov); err != nil {
			return fmt.Errorf("parsing %s: %w", provPath, err)
		}
		cfg.Plugins = prov.Plugins
		cfg.MCPServers = prov.MCPServers
		cfg.Hooks = prov.Hooks
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("reading %s: %w", provPath, err)
	}

	// 3. Load profiles/*.yaml if present
	for _, pFile := range listProfileFiles(configDir) {
		name := strings.TrimSuffix(filepath.Base(pFile), ".yaml")
		if !gatewayIdentRE.MatchString(name) {
			return fmt.Errorf("%w %s: profile name %q must be letters, digits, '-' or '_'", ErrInvalidProfileFilename, pFile, name)
		}
		if _, exists := cfg.Contexts[name]; exists {
			return fmt.Errorf("%w: profile %q is defined in both config.yaml and profiles/%s.yaml; run 'aide config migrate --write' to consolidate",
				ErrDuplicateProfile, name, name)
		}
		data, err := os.ReadFile(pFile)
		if err != nil {
			return fmt.Errorf("reading %s: %w", pFile, err)
		}
		var node yaml.Node
		if err := yaml.Unmarshal(data, &node); err != nil {
			return fmt.Errorf("parsing %s: %w", pFile, err)
		}
		ctx, err := DecodeProfileYAML(name, &node)
		if err != nil {
			return fmt.Errorf("parsing %s: %w", pFile, err)
		}
		if cfg.Contexts == nil {
			cfg.Contexts = make(map[string]Context)
		}
		cfg.Contexts[name] = ctx
	}

	cfg.applyDefaultProfileDirs()
	return nil
}

func checkAllowedKeys(doc *yaml.Node, allowed []string, path string) error {
	for _, k := range mappingKeys(doc) {
		if !slices.Contains(allowed, k) {
			return fmt.Errorf("parsing %s: unknown or misplaced key %q (allowed: %s)", path, k, strings.Join(allowed, ", "))
		}
	}
	return nil
}

// SplitRelativePaths returns the relative paths (using forward slashes) of
// all active modular config files present under configDir: "sandbox.yaml",
// "provisioning.yaml", and sorted "profiles/*.yaml" (excluding hidden files
// and *.bak* backups).
func SplitRelativePaths(configDir string) []string {
	var out []string
	for _, name := range []string{"sandbox.yaml", "provisioning.yaml"} {
		if _, err := os.Stat(filepath.Join(configDir, name)); err == nil {
			out = append(out, name)
		}
	}
	for _, pFile := range listProfileFiles(configDir) {
		out = append(out, "profiles/"+filepath.Base(pFile))
	}
	return out
}

// listProfileFiles returns sorted absolute paths of active *.yaml profile files
// inside configDir/profiles, skipping hidden files (.*) and backups (*.bak*).
func listProfileFiles(configDir string) []string {
	profDir := filepath.Join(configDir, "profiles")
	entries, err := os.ReadDir(profDir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, ".") || strings.Contains(name, ".bak") || !strings.HasSuffix(name, ".yaml") {
			continue
		}
		out = append(out, filepath.Join(profDir, name))
	}
	slices.Sort(out)
	return out
}
