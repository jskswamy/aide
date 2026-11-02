package provision

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jskswamy/aide/internal/config"
)

// ConfigHash returns "sha256:<hex>" for the configuration rooted at path.
// When path is "config.yaml" and any modular split files (sandbox.yaml,
// provisioning.yaml, or profiles/*.yaml) exist in filepath.Dir(path),
// ConfigHash computes a composite hash over all present config files so
// editing any split file triggers drift detection. If no config file
// exists, returns ("", nil).
func ConfigHash(path string) (string, error) {
	if filepath.Base(path) != "config.yaml" {
		return singleFileHash(path)
	}
	dir := filepath.Dir(path)
	splitRel := config.SplitRelativePaths(dir)
	if len(splitRel) == 0 {
		return singleFileHash(path)
	}

	rels := make([]string, 0, 1+len(splitRel))
	if _, err := os.Stat(path); err == nil {
		rels = append(rels, "config.yaml")
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("provision: hashing config %s: %w", path, err)
	}
	rels = append(rels, splitRel...)

	h := sha256.New()
	for _, rel := range rels {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		data, err := os.ReadFile(full)
		if err != nil {
			return "", fmt.Errorf("provision: hashing config %s: %w", full, err)
		}
		_, _ = h.Write([]byte(rel))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write(data)
		_, _ = h.Write([]byte{0})
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

func singleFileHash(path string) (string, error) {
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
// (via singleFileHash, which already treats a missing file as "" — no
// separate sentinel needed), or "" if the context has no secret
// configured. Used both to persist a drift signal after a successful
// sync and, by the sync secrets hash gate, to decide whether a sync
// run can skip decryption entirely.
func ContextSecretsHash(ctx config.Context) (string, error) {
	if ctx.Secret == "" {
		return "", nil
	}
	return singleFileHash(config.ResolveSecretPath(ctx.Secret))
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
