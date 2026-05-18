package provision

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
)

// ConfigHash returns "sha256:<hex>" for the bytes of path. If path
// does not exist, returns ("", nil) so the launch drift check can
// treat a missing config as "no drift" (first run).
func ConfigHash(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", fmt.Errorf("provision: hashing config %s: %w", path, err)
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
