package secrets

import (
	"maps"
	"os"
	"slices"

	"gopkg.in/yaml.v3"
)

// KeyNames returns the sorted top-level key names of the YAML or JSON
// sops store at filePath without decrypting it (sops leaves key names
// in clear), excluding the sops metadata key. known is false when the
// file is unreadable or in another format.
func KeyNames(filePath string) (names []string, known bool) {
	data, err := os.ReadFile(resolveSecretsPath(filePath))
	if err != nil {
		return nil, false
	}
	var raw map[string]yaml.Node
	if err := yaml.Unmarshal(data, &raw); err != nil || raw == nil {
		return nil, false
	}
	delete(raw, "sops")
	return slices.Sorted(maps.Keys(raw)), true
}
