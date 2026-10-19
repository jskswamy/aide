package capability

import (
	"os"
	"path/filepath"
	"slices"

	"github.com/jskswamy/aide/internal/homepath"
	"github.com/jskswamy/aide/internal/provision/skillshare"
)

const (
	ccstatuslineSettingsPath = "~/.config/ccstatusline/settings.json"
	bivouacConfigDir         = "~/.config/bivouac"
	ponytailConfigDir        = "~/.config/ponytail"
	// Skillshare's installed plugins; it is how a fresh install shows up
	// before ponytail has written its own config dir.
	ponytailPluginGlob = "~/.local/state/skillshare/plugins/*/skillshare-ponytail-*"
)

// AutoInclude adds the capabilities aide turns on by itself when their
// tool is set up on this machine:
//
//   - "ccstatusline" when its settings file exists, so a sandboxed agent
//     process invoking `aide statusline claude` from a ccstatusline
//     Custom Command widget can read its config;
//   - "bivouac" when its config dir exists, so `bivouac list` and `status`
//     can read their config and state;
//   - "ponytail" when its config dir exists or Skillshare has it installed,
//     so its band can write the task ledger;
//   - "skillshare" when Skillshare's config dir exists, so the agent can
//     read the skills, agents and plugins Skillshare links into its
//     config dir.
//
// A capability already in capNames is not added twice, and one named in
// excluded (--without and the project's disabled_capabilities) is not
// added at all. getenv reads XDG_CONFIG_HOME for Skillshare's dir.
//
// Lives in internal/capability (rather than internal/launcher) so it can be
// called from every place a context's effective capability set is computed
// — both actual agent launch (internal/launcher) and read-only inspection
// (`aide cap list`, `aide cap audit` in cmd/aide) — keeping what those
// commands report in sync with what a real launch actually grants.
func AutoInclude(capNames, excluded []string, homeDir string, getenv func(string) string) []string {
	_, err := os.Stat(homepath.Expand(ccstatuslineSettingsPath, homeDir))
	capNames = autoInclude(capNames, excluded, "ccstatusline", err == nil)
	_, err = os.Stat(homepath.Expand(bivouacConfigDir, homeDir))
	capNames = autoInclude(capNames, excluded, "bivouac", err == nil)
	capNames = autoInclude(capNames, excluded, "ponytail", ponytailInstalled(homeDir))
	return autoInclude(capNames, excluded, "skillshare", skillshare.Installed(homeDir, getenv))
}

func autoInclude(capNames, excluded []string, name string, installed bool) []string {
	if !installed || slices.Contains(capNames, name) || slices.Contains(excluded, name) {
		return capNames
	}
	return append(capNames, name)
}

func ponytailInstalled(homeDir string) bool {
	if _, err := os.Stat(homepath.Expand(ponytailConfigDir, homeDir)); err == nil {
		return true
	}
	m, _ := filepath.Glob(homepath.Expand(ponytailPluginGlob, homeDir))
	return len(m) > 0
}
