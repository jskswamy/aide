package launcher

import (
	"os"

	"github.com/jskswamy/aide/internal/homepath"
)

const ccstatuslineSettingsPath = "~/.config/ccstatusline/settings.json"

// autoIncludeCcstatusline adds the "ccstatusline" capability to capNames
// when the tool's settings file exists on disk, so a sandboxed agent
// process invoking `aide statusline claude` from a ccstatusline Custom
// Command widget can read its config. No-ops when the capability is
// already present, was explicitly excluded via --without, or the settings
// file doesn't exist (ccstatusline isn't installed/configured).
func autoIncludeCcstatusline(capNames, withoutCaps []string, homeDir string) []string {
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
