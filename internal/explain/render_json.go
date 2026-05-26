package explain

import (
	"encoding/json"
	"fmt"
)

// RenderJSON renders the document as indented JSON.
func RenderJSON(doc Document) (string, error) {
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshaling explain document: %w", err)
	}
	return string(b), nil
}
