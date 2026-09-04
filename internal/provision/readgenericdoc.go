package provision

import (
	"errors"
	"fmt"
	"os"
)

// ReadGenericDoc reads path and unmarshals it into a generic document
// map via unmarshal, treating a missing file as an empty document.
// agentName prefixes wrapped errors (e.g. "codex", "opencode").
func ReadGenericDoc(agentName, path string, unmarshal func([]byte) (map[string]any, error)) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]any{}, nil
		}
		return nil, fmt.Errorf("%s: reading config %s: %w", agentName, path, err)
	}
	doc, err := unmarshal(data)
	if err != nil {
		return nil, fmt.Errorf("%s: parsing config %s: %w", agentName, path, err)
	}
	return doc, nil
}
