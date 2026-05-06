// Package secrets handles age-based encryption and decryption of secret files.
package secrets

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// AgeKeySource describes where an age key was found.
type AgeKeySource int

// AgeKeySource values identify where an age key was discovered.
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

	// 4. Default key path: OS-canonical sops location, with XDG fallback on macOS.
	for _, p := range defaultKeyPaths() {
		if fileReadable(p) {
			return &AgeIdentity{Source: SourceDefaultFile, KeyData: p}, nil
		}
	}

	return nil, fmt.Errorf(`no age identity found. aide needs an age key to decrypt secrets.

Options:
  1. Plug in a YubiKey with age-plugin-yubikey installed
  2. Set SOPS_AGE_KEY env var (for CI/Docker)
  3. Set SOPS_AGE_KEY_FILE to point to your key file
  4. Run: age-keygen -o %s

Run 'aide setup' for guided configuration`, defaultKeyPath())
}

// defaultKeyPath returns the OS-canonical sops age key location for messages.
// On macOS this is ~/Library/Application Support/sops/age/keys.txt; on Linux
// it honors $XDG_CONFIG_HOME (or ~/.config). Matches sops upstream behavior.
func defaultKeyPath() string {
	paths := defaultKeyPaths()
	if len(paths) == 0 {
		return filepath.Join("~", ".config", "sops", "age", "keys.txt")
	}
	return paths[0]
}

// defaultKeyPaths returns candidate sops age key locations in priority order.
// The first entry is the OS-canonical path (per Go's os.UserConfigDir, matching
// sops). On macOS, the XDG-style ~/.config path is appended as a secondary
// candidate so users with cross-platform setups still resolve their key.
func defaultKeyPaths() []string {
	var paths []string

	if dir, err := os.UserConfigDir(); err == nil {
		paths = append(paths, filepath.Join(dir, "sops", "age", "keys.txt"))
	}

	if runtime.GOOS == "darwin" {
		if home, err := os.UserHomeDir(); err == nil {
			xdg := filepath.Join(home, ".config", "sops", "age", "keys.txt")
			if len(paths) == 0 || paths[0] != xdg {
				paths = append(paths, xdg)
			}
		}
	}

	return paths
}

// fileReadable returns true if the path exists and is a regular file.
func fileReadable(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}
