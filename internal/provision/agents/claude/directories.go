package claude

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/jskswamy/aide/internal/fsutil"
	"github.com/jskswamy/aide/internal/provision"
)

var _ provision.DirectoryGranter = (*Driver)(nil)

// directorySettingsPath returns the settings.json aide should edit for
// a directory grant. Project-scoped calls (ctx.ProjectRoot set) target
// the project-local settings.local.json, matching how Claude itself
// splits project vs user settings. Global-scoped calls fall back to
// the same profile-aware path hooks/statusline already use.
func directorySettingsPath(ctx provision.Context) string {
	if ctx.ProjectRoot != "" {
		return filepath.Join(ctx.ProjectRoot, ".claude", "settings.local.json")
	}
	return settingsPath(ctx)
}

// GrantDirectory implements provision.DirectoryGranter. write is
// accepted for interface parity but unused: Claude's
// permissions.additionalDirectories does not distinguish read/write
// access.
func (d *Driver) GrantDirectory(ctx provision.Context, path string, _ bool) error {
	settingsFile := directorySettingsPath(ctx)
	raw, err := readSettingsFile(settingsFile)
	if err != nil {
		return err
	}
	dirs := readAdditionalDirectories(raw)
	for _, existing := range dirs {
		if existing == path {
			return nil
		}
	}
	writeAdditionalDirectories(raw, append(dirs, path))
	return writeSettingsFile(settingsFile, raw)
}

// RevokeDirectory implements provision.DirectoryGranter. It removes
// any additionalDirectories entry equal to path or nested under it;
// entries that are merely a parent of path are left untouched.
func (d *Driver) RevokeDirectory(ctx provision.Context, path string) error {
	settingsFile := directorySettingsPath(ctx)
	raw, err := readSettingsFile(settingsFile)
	if err != nil {
		return err
	}
	dirs := readAdditionalDirectories(raw)
	kept := make([]string, 0, len(dirs))
	changed := false
	for _, existing := range dirs {
		if isWithin(path, existing) {
			changed = true
			continue
		}
		kept = append(kept, existing)
	}
	if !changed {
		return nil
	}
	writeAdditionalDirectories(raw, kept)
	return writeSettingsFile(settingsFile, raw)
}

// readAdditionalDirectories returns permissions.additionalDirectories
// as a []string, or nil if unset/malformed.
func readAdditionalDirectories(raw map[string]interface{}) []string {
	perms, _ := raw["permissions"].(map[string]interface{})
	rawDirs, _ := perms["additionalDirectories"].([]interface{})
	out := make([]string, 0, len(rawDirs))
	for _, dv := range rawDirs {
		if s, ok := dv.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// writeAdditionalDirectories sets permissions.additionalDirectories,
// creating the permissions object if needed and preserving its other
// keys (allow, deny, etc.).
func writeAdditionalDirectories(raw map[string]interface{}, dirs []string) {
	perms, ok := raw["permissions"].(map[string]interface{})
	if !ok {
		perms = map[string]interface{}{}
		raw["permissions"] = perms
	}
	list := make([]interface{}, len(dirs))
	for i, dv := range dirs {
		list[i] = dv
	}
	perms["additionalDirectories"] = list
}

// isWithin reports whether child is parent itself or nested under it.
// Uses filepath.Rel the same way pathUnderHome in
// pkg/seatbelt/guards/guard_git_integration.go does, rather than a
// string-prefix check, so a sibling that merely shares a prefix (e.g.
// /foo/bar vs /foo/barbaz) is not misclassified as nested.
func isWithin(parent, child string) bool {
	if parent == child {
		return true
	}
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func writeSettingsFile(path string, raw map[string]interface{}) error {
	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return fmt.Errorf("claude settings: marshal: %w", err)
	}
	return fsutil.AtomicWrite(path, data)
}
