package secrets

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// AgeKeySource describes where an age key was found.
type AgeKeySource int

const (
	SourceYubiKey     AgeKeySource = iota
	SourceEnvKey                        // SOPS_AGE_KEY
	SourceEnvKeyFile                    // SOPS_AGE_KEY_FILE
	SourceDefaultFile                   // XDG default path
)

// AgeIdentity holds a discovered age identity for sops decryption.
type AgeIdentity struct {
	Source AgeKeySource
	// KeyData contains the raw key material for SourceEnvKey,
	// or the file path for SourceEnvKeyFile / SourceDefaultFile.
	// For SourceYubiKey this is empty (plugin handles it).
	KeyData string
}

// DiscoverAgeKey searches for an age identity in priority order.
// Returns the first identity found, or an error with guidance
// if no identity is available.
func DiscoverAgeKey() (*AgeIdentity, error) {
	// 1. YubiKey: detect age-plugin-yubikey on PATH.
	if _, err := exec.LookPath("age-plugin-yubikey"); err == nil {
		return &AgeIdentity{Source: SourceYubiKey}, nil
	}

	// 2. SOPS_AGE_KEY environment variable.
	if val := os.Getenv("SOPS_AGE_KEY"); val != "" {
		if strings.HasPrefix(val, "AGE-SECRET-KEY-") {
			return &AgeIdentity{Source: SourceEnvKey, KeyData: val}, nil
		}
		// Invalid prefix — skip silently, fall through to next source.
	}

	// 3. SOPS_AGE_KEY_FILE environment variable.
	if val := os.Getenv("SOPS_AGE_KEY_FILE"); val != "" {
		if fileReadable(val) {
			return &AgeIdentity{Source: SourceEnvKeyFile, KeyData: val}, nil
		}
		// File missing or unreadable — skip silently.
	}

	// 4. Default key path: $XDG_CONFIG_HOME/sops/age/keys.txt
	defaultPath := defaultKeyPath()
	if fileReadable(defaultPath) {
		return &AgeIdentity{Source: SourceDefaultFile, KeyData: defaultPath}, nil
	}

	return nil, fmt.Errorf(`no age identity found. aide needs an age key to decrypt secrets.

Options:
  1. Plug in a YubiKey with age-plugin-yubikey installed
  2. Set SOPS_AGE_KEY env var (for CI/Docker)
  3. Set SOPS_AGE_KEY_FILE to point to your key file
  4. Run: age-keygen -o %s

Run 'aide setup' for guided configuration`, defaultPath)
}

// defaultKeyPath returns the standard sops age key location.
func defaultKeyPath() string {
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = "~"
		}
		configHome = filepath.Join(home, ".config")
	}
	return filepath.Join(configHome, "sops", "age", "keys.txt")
}

// fileReadable returns true if the path exists and is a regular file.
func fileReadable(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}
