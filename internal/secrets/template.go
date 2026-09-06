package secrets

import "fmt"

// LoadSecretsMap discovers the local age identity and decrypts the
// sops-encrypted file at secretsPath, returning its plaintext
// key/value map. Shared by `aide launch` (env injection at process
// start) and `aide sync` (MCP server env template resolution) —
// previously each independently reimplemented this discover+decrypt
// sequence.
func LoadSecretsMap(secretsPath string) (map[string]string, error) {
	identity, err := DiscoverAgeKey()
	if err != nil {
		return nil, fmt.Errorf("discovering age key: %w", err)
	}
	secretsMap, err := DecryptSecretsFile(secretsPath, identity)
	if err != nil {
		return nil, fmt.Errorf("decrypting secrets: %w", err)
	}
	return secretsMap, nil
}
