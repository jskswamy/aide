package config

import (
	"fmt"
	"path/filepath"

	"github.com/jskswamy/aide/internal/fsutil"
	"github.com/jskswamy/aide/internal/trust"
	"gopkg.in/yaml.v3"
)

// WriteProjectOverride writes the given ProjectOverride to the given path atomically.
func WriteProjectOverride(path string, po *ProjectOverride) error {
	data, err := yaml.Marshal(po)
	if err != nil {
		return fmt.Errorf("marshaling project override: %w", err)
	}
	return fsutil.AtomicWrite(path, data)
}

// WriteProjectOverrideWithTrust writes the project override and auto-re-trusts the file
// if it was previously trusted. This prevents users from being re-prompted after
// aide-initiated modifications (e.g., `aide cap enable docker`).
//
// Security: only auto-re-trusts if the pre-modification content was trusted.
// If the file was externally modified after the last trust, the pre-modification
// content won't match any trust hash, so we skip auto-re-trust.
func WriteProjectOverrideWithTrust(path string, po *ProjectOverride, store *trust.Store) error {
	// Check pre-modification trust status.
	before, readErr := store.CheckFile(path)
	wasTrusted := readErr == nil && before.Status == trust.Trusted

	// Write the new content using existing write function.
	if err := WriteProjectOverride(path, po); err != nil {
		return err
	}

	// Auto-re-trust if previously trusted.
	if wasTrusted {
		if after, err := store.CheckFile(path); err == nil {
			_ = store.Trust(after.Path, after.Contents)
		}
	}

	return nil
}

// FindProjectConfigForWrite returns the .aide.yaml that project-scope
// edits from startDir write: this checkout's existing file, else one
// at the checkout root, else in startDir. In a linked worktree that is
// the worktree's own file, never the inherited main checkout's.
func FindProjectConfigForWrite(startDir string) string {
	return DiscoverProject(startDir).LocalWritePath(startDir)
}

// ProjectConfigWriteTarget is FindProjectConfigForWrite, or with main
// the main checkout's .aide.yaml of the linked worktree startDir is in
// (created if absent). main outside a linked worktree is an error.
func ProjectConfigWriteTarget(startDir string, main bool) (string, error) {
	d := DiscoverProject(startDir)
	if !main {
		return d.LocalWritePath(startDir), nil
	}
	if d.Err != nil {
		return "", d.Err
	}
	if d.MainCheckoutRoot == "" {
		return "", fmt.Errorf("--main needs a linked git worktree; %s is not in one (edit its .aide.yaml without --main)", startDir)
	}
	return filepath.Join(d.MainCheckoutRoot, ProjectConfigFileName), nil
}
