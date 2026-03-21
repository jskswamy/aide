package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/adrg/xdg"
)

const appName = "aide"

// configHome returns the base config directory, preferring ~/.config on
// macOS when $XDG_CONFIG_HOME is not explicitly set. The adrg/xdg library
// follows Apple conventions (~/Library/Application Support) but CLI tools
// conventionally use ~/.config on all platforms.
func configHome() string {
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return v
	}
	if runtime.GOOS == "darwin" {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, ".config")
		}
	}
	return xdg.ConfigHome
}

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

// ResolveSecretPathFrom resolves a secret name to an absolute path.
// If the value is already absolute, return it as-is.
// Otherwise, resolve relative to SecretsDirFrom(base).
// Bare names (e.g. "work") get ".enc.yaml" appended automatically.
func ResolveSecretPathFrom(base, secret string) string {
	if filepath.IsAbs(secret) {
		return secret
	}
	if !strings.HasSuffix(secret, ".enc.yaml") {
		secret = secret + ".enc.yaml"
	}
	return filepath.Join(SecretsDirFrom(base), secret)
}

// --- Convenience wrappers (use adrg/xdg cached values) ---

// ConfigDir returns the aide config directory.
// $XDG_CONFIG_HOME/aide/ (typically ~/.config/aide/)
// Everything lives here: config.yaml and secrets/ (DD-6).
func ConfigDir() string {
	return ConfigDirFrom(configHome())
}

// SecretsDir returns the directory for encrypted secrets files.
// $XDG_CONFIG_HOME/aide/secrets/
func SecretsDir() string {
	return SecretsDirFrom(configHome())
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
	return ConfigFilePathFrom(configHome())
}

// ProjectConfigFileName is the per-project override filename.
const ProjectConfigFileName = ".aide.yaml"

// ResolveSecretPath resolves a secret name to an absolute path.
// If the value is already absolute, return it as-is.
// Otherwise, resolve relative to SecretsDir().
// Bare names (e.g. "work") get ".enc.yaml" appended automatically.
func ResolveSecretPath(secret string) string {
	return ResolveSecretPathFrom(configHome(), secret)
}
