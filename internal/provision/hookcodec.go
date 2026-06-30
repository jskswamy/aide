package provision

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// HookCodec is the Template Method interface for hook read-modify-write.
// Codecs vary (serialize/deserialize/removal), the skeleton does not.
type HookCodec interface {
	// Match reports whether name is an artifact owned by this codec.
	// Reuses HookArtifact.Owns logic.
	Match(name string) bool

	// Decode reads an artifact at path and returns the Hook.
	// path is the artifact (file or directory); codec owns deserialization.
	Decode(path string) (Hook, error)

	// Encode writes a Hook as an artifact under dir.
	// codec owns serialization, post-write side effects (e.g., chmod), and
	// artifact naming (dir contains the artifact at codec.Name(h.Command)).
	Encode(dir string, h Hook) error

	// Remove deletes the artifact at path (file or directory).
	// Codec owns both os.Remove (file) and os.RemoveAll (dir).
	Remove(path string) error
}

// ReadHooks returns aide-managed hooks by listing dir and filtering via codec.Match.
// Returns nil with no error if dir does not exist (standard pattern for hook hooks).
func ReadHooks(dir string, codec HookCodec) ([]Hook, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("readdir: %w", err)
	}
	var out []Hook
	for _, e := range entries {
		if !codec.Match(e.Name()) {
			continue
		}
		hook, err := codec.Decode(filepath.Join(dir, e.Name()))
		if err != nil {
			continue // skip malformed artifacts
		}
		out = append(out, hook)
	}
	return out, nil
}

// WriteHooks reconciles desired hooks into dir, removing all codec-owned artifacts
// and writing new ones. Idempotent: safe to call repeatedly with same desired slice.
func WriteHooks(dir string, desired []Hook, codec HookCodec) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}

	// Remove all existing codec-owned artifacts.
	if existing, err := os.ReadDir(dir); err == nil {
		for _, e := range existing {
			if codec.Match(e.Name()) {
				if err := codec.Remove(filepath.Join(dir, e.Name())); err != nil {
					return fmt.Errorf("remove: %w", err)
				}
			}
		}
	}

	// Write new artifacts for desired hooks.
	for _, h := range desired {
		if err := codec.Encode(dir, h); err != nil {
			return err
		}
	}
	return nil
}
