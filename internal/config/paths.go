package config

import (
	"fmt"
	"path/filepath"

	"github.com/adrg/xdg"
)

const appName = "aide"

// --- Parameterized variants (testable, no dependency on cached xdg values) ---

// ConfigDirFrom returns the aide config directory under the given base.
// Production callers pass xdg.ConfigHome; tests pass a temp dir.
func ConfigDirFrom(base string) string {
	return filepath.Join(base, appName)
}

// SecretsDirFrom returns the secrets directory under the given base.
func SecretsDirFrom(base string) string {
	return filepath.Join(ConfigDirFrom(base), "secrets")
}

// RuntimeDirFrom returns a per-process ephemeral directory under the given base.
func RuntimeDirFrom(base string, pid int) string {
	return filepath.Join(base, fmt.Sprintf("%s-%d", appName, pid))
}

// ConfigFilePathFrom returns the global config file path under the given base.
func ConfigFilePathFrom(base string) string {
	return filepath.Join(ConfigDirFrom(base), "config.yaml")
}

// ResolveSecretsFilePathFrom resolves a secrets_file value to an absolute path.
// If the value is already absolute, return it as-is.
// Otherwise, resolve relative to SecretsDirFrom(base).
func ResolveSecretsFilePathFrom(base, secretsFile string) string {
	if filepath.IsAbs(secretsFile) {
		return secretsFile
	}
	return filepath.Join(SecretsDirFrom(base), secretsFile)
}

// --- Convenience wrappers (use adrg/xdg cached values) ---

// ConfigDir returns the aide config directory.
// $XDG_CONFIG_HOME/aide/ (typically ~/.config/aide/)
// Everything lives here: config.yaml and secrets/ (DD-6).
func ConfigDir() string {
	return ConfigDirFrom(xdg.ConfigHome)
}

// SecretsDir returns the directory for encrypted secrets files.
// $XDG_CONFIG_HOME/aide/secrets/
func SecretsDir() string {
	return SecretsDirFrom(xdg.ConfigHome)
}

// RuntimeDir returns a per-process ephemeral directory on tmpfs.
// $XDG_RUNTIME_DIR/aide-<pid>/
// Used for generated MCP configs and other material that must not persist (DD-10).
// The caller is responsible for creating and cleaning this directory.
func RuntimeDir(pid int) string {
	return RuntimeDirFrom(xdg.RuntimeDir, pid)
}

// ConfigFilePath returns the path to the global config file.
// $XDG_CONFIG_HOME/aide/config.yaml
func ConfigFilePath() string {
	return ConfigFilePathFrom(xdg.ConfigHome)
}

// ProjectConfigFileName is the per-project override filename.
const ProjectConfigFileName = ".aide.yaml"

// ResolveSecretsFilePath resolves a secrets_file value to an absolute path.
// If the value is already absolute, return it as-is.
// Otherwise, resolve relative to SecretsDir().
func ResolveSecretsFilePath(secretsFile string) string {
	return ResolveSecretsFilePathFrom(xdg.ConfigHome, secretsFile)
}
