package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/jskswamy/aide/internal/trust"
	"gopkg.in/yaml.v3"
)

// WriteProjectOverride writes the given ProjectOverride to the given path atomically.
func WriteProjectOverride(path string, po *ProjectOverride) error {
	data, err := yaml.Marshal(po)
	if err != nil {
		return fmt.Errorf("marshaling project override: %w", err)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("creating directory: %w", err)
	}

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o600); err != nil {
		return fmt.Errorf("writing temp file: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("renaming temp file: %w", err)
	}
	return nil
}

// WriteProjectOverrideWithTrust writes the project override and auto-re-trusts the file
// if it was previously trusted. This prevents users from being re-prompted after
// aide-initiated modifications (e.g., `aide cap enable docker`).
//
// Security: only auto-re-trusts if the pre-modification content was trusted.
// If the file was externally modified after the last trust, the pre-modification
// content won't match any trust hash, so we skip auto-re-trust.
func WriteProjectOverrideWithTrust(path string, po *ProjectOverride, store *trust.Store) error {
	absPath, err := filepath.Abs(path)
	if err != nil {
		absPath = path
	}

	// Check pre-modification trust status.
	oldContents, readErr := os.ReadFile(absPath)
	wasTrusted := readErr == nil && store.Check(absPath, oldContents) == trust.Trusted

	// Write the new content using existing write function.
	if err := WriteProjectOverride(path, po); err != nil {
		return err
	}

	// Auto-re-trust if previously trusted.
	if wasTrusted {
		newContents, err := os.ReadFile(absPath)
		if err == nil {
			_ = store.Trust(absPath, newContents)
		}
	}

	return nil
}

// FindProjectConfigForWrite walks up from startDir to find where to write .aide.yaml.
// 1. If .aide.yaml exists in any ancestor (up to git root), return its path.
// 2. If a .git directory is found (no .aide.yaml), return .aide.yaml in that dir.
// 3. If neither found, return .aide.yaml in startDir.
func FindProjectConfigForWrite(startDir string) string {
	dir := startDir
	for {
		candidate := filepath.Join(dir, ProjectConfigFileName)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}

		gitDir := filepath.Join(dir, ".git")
		if _, err := os.Stat(gitDir); err == nil {
			return filepath.Join(dir, ProjectConfigFileName)
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return filepath.Join(startDir, ProjectConfigFileName)
		}
		dir = parent
	}
}
