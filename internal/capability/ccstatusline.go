package capability

import (
	"os"

	"github.com/jskswamy/aide/internal/homepath"
)

const ccstatuslineSettingsPath = "~/.config/ccstatusline/settings.json"

// AutoIncludeCcstatusline adds the "ccstatusline" capability to capNames
// when the tool's settings file exists on disk, so a sandboxed agent
// process invoking `aide statusline claude` from a ccstatusline Custom
// Command widget can read its config. No-ops when the capability is
// already present, was explicitly excluded via --without, or the settings
// file doesn't exist (ccstatusline isn't installed/configured).
//
// Lives in internal/capability (rather than internal/launcher) so it can be
// called from every place a context's effective capability set is computed
// — both actual agent launch (internal/launcher) and read-only inspection
// (`aide cap list`, `aide cap audit` in cmd/aide) — keeping what those
// commands report in sync with what a real launch actually grants.
func AutoIncludeCcstatusline(capNames, withoutCaps []string, homeDir string) []string {
	for _, c := range capNames {
		if c == "ccstatusline" {
			return capNames
		}
	}
	for _, c := range withoutCaps {
		if c == "ccstatusline" {
			return capNames
		}
	}
	if _, err := os.Stat(homepath.Expand(ccstatuslineSettingsPath, homeDir)); err != nil {
		return capNames
	}
	return append(capNames, "ccstatusline")
}
