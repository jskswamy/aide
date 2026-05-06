package secrets

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	sops "github.com/getsops/sops/v3"
	"github.com/getsops/sops/v3/decrypt"
	"gopkg.in/yaml.v3"
)

// DecryptSecretsFile decrypts a sops-encrypted YAML file and returns
// the key-value pairs as a flat string map.
//
// The filePath is resolved relative to $XDG_CONFIG_HOME/aide/secrets/
// unless it is an absolute path. The AgeIdentity is used to set up
// the environment for sops decryption.
func DecryptSecretsFile(filePath string, identity *AgeIdentity) (map[string]string, error) {
	absPath := resolveSecretsPath(filePath)

	// Set up environment for sops decryption based on identity source.
	cleanup, err := setupDecryptEnv(identity)
	if err != nil {
		return nil, fmt.Errorf("failed to set up decryption environment for %s: %w", filePath, err)
	}
	defer cleanup()

	// Decrypt using sops library.
	decrypted, err := decrypt.File(absPath, "yaml")
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt %s: %s", filePath, sopsErrorDetail(err))
	}

	// Unmarshal into map[string]interface{} first, then validate and flatten.
	var raw map[string]interface{}
	if err := yaml.Unmarshal(decrypted, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse decrypted YAML from %s: %w", filePath, err)
	}

	// Convert to map[string]string, rejecting non-scalar values.
	result := make(map[string]string, len(raw))
	for k, v := range raw {
		switch val := v.(type) {
		case string:
			result[k] = val
		case nil:
			result[k] = ""
		case int, int64, float64, bool:
			result[k] = fmt.Sprintf("%v", val)
		default:
			return nil, fmt.Errorf(
				"secrets file %s contains non-string value for key %q, only flat key-value pairs are supported",
				filePath, k,
			)
		}
	}

	return result, nil
}

// resolveSecretsPath resolves the file path. If absolute, returns as-is.
// If relative, joins with $XDG_CONFIG_HOME/aide/secrets/.
func resolveSecretsPath(filePath string) string {
	if filepath.IsAbs(filePath) {
		return filePath
	}

	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = "~"
		}
		configHome = filepath.Join(home, ".config")
	}

	return filepath.Join(configHome, "aide", "secrets", filePath)
}

// sopsErrorDetail extracts the detailed error message from a sops decryption
// error. If the error implements sops.UserError, the detailed per-key failure
// information is returned instead of the terse summary.
func sopsErrorDetail(err error) string {
	var ue sops.UserError
	if errors.As(err, &ue) {
		return ue.UserError()
	}
	return err.Error()
}

// setupDecryptEnv configures environment variables for sops decryption
// based on the AgeIdentity source. Returns a cleanup function that
// restores the original environment.
func setupDecryptEnv(identity *AgeIdentity) (func(), error) {
	var restores []func()
	noop := func() {}

	setEnv := func(key, value string) error {
		orig, existed := os.LookupEnv(key)
		if err := os.Setenv(key, value); err != nil {
			return err
		}
		restores = append(restores, func() {
			if existed {
				_ = os.Setenv(key, orig)
			} else {
				_ = os.Unsetenv(key)
			}
		})
		return nil
	}

	cleanup := func() {
		for i := len(restores) - 1; i >= 0; i-- {
			restores[i]()
		}
	}

	switch identity.Source {
	case SourceYubiKey:
		// age-plugin-yubikey handles age1yubikey1... recipients automatically
		// via PATH discovery. Also surface the default keys.txt if present so
		// software identities in the same file can decrypt regular age1...
		// recipients (users commonly mix both kinds in one keys.txt).
		// Skip if SOPS_AGE_KEY_FILE is already set explicitly — respect the
		// caller's choice.
		if os.Getenv("SOPS_AGE_KEY_FILE") == "" {
			for _, p := range defaultKeyPaths() {
				if fileReadable(p) {
					if err := setEnv("SOPS_AGE_KEY_FILE", p); err != nil {
						return noop, err
					}
					break
				}
			}
		}
		return cleanup, nil

	case SourceEnvKey:
		// Set SOPS_AGE_KEY to the raw key material.
		if err := setEnv("SOPS_AGE_KEY", identity.KeyData); err != nil {
			return noop, err
		}
		// Clear SOPS_AGE_KEY_FILE to avoid conflicts.
		if err := setEnv("SOPS_AGE_KEY_FILE", ""); err != nil {
			cleanup()
			return noop, err
		}
		return cleanup, nil

	case SourceEnvKeyFile, SourceDefaultFile:
		// Set SOPS_AGE_KEY_FILE to the key file path.
		if err := setEnv("SOPS_AGE_KEY_FILE", identity.KeyData); err != nil {
			return noop, err
		}
		// Clear SOPS_AGE_KEY to avoid conflicts.
		if err := setEnv("SOPS_AGE_KEY", ""); err != nil {
			cleanup()
			return noop, err
		}
		return cleanup, nil

	default:
		return noop, fmt.Errorf("unknown age identity source: %d", identity.Source)
	}
}
