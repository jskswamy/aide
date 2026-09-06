package provision

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"

	"github.com/jskswamy/aide/internal/config"
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

// ContextSecretsHash returns the hash of ctx's encrypted secrets file
// (via ConfigHash, which already treats a missing file as "" — no
// separate sentinel needed), or "" if the context has no secret
// configured. Used both to persist a drift signal after a successful
// sync and, by the sync secrets hash gate, to decide whether a sync
// run can skip decryption entirely.
func ContextSecretsHash(ctx config.Context) (string, error) {
	if ctx.Secret == "" {
		return "", nil
	}
	return ConfigHash(config.ResolveSecretPath(ctx.Secret))
}

// ConfigUnchanged reports whether cur and stored are both non-empty
// and equal — the shared "has this recorded config hash drifted"
// predicate used by both DriftStatus (the launch-time banner) and the
// sync secrets hash gate (aide sync's decrypt-skip decision). Two
// empty hashes are NOT considered unchanged: an empty hash means the
// file was missing when computed, an anomaly neither caller should
// treat as steady state.
func ConfigUnchanged(cur, stored string) bool {
	return cur != "" && stored != "" && cur == stored
}
