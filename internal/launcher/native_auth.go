package launcher

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jskswamy/aide/internal/config"
)

// nativeAuthNotice describes an authentication source in the selected
// profile's native config that aide cannot scrub, or "" when there is
// none. harnessName is the canonical harness the launch runs
// (config.HarnessOf, so agent aliases count). Only launches that select
// their own config dir are checked.
func nativeAuthNotice(harnessName string, ctx config.Context, home string) string {
	if !ctx.SelectsConfigDir() || harnessName != "claude" {
		return ""
	}
	return claudeAPIKeyHelperNotice(ctx.NativeConfigDir(harnessName, home))
}

// claudeAPIKeyHelperNotice reports an apiKeyHelper in dir/settings.json
// (Claude Code's user settings under CLAUDE_CONFIG_DIR). The helper is
// a command Claude runs for a key, so no environment scrub reaches it.
// The notice names the setting, never its command.
func claudeAPIKeyHelperNotice(dir string) string {
	path := filepath.Join(dir, "settings.json")
	data, err := os.ReadFile(path) // #nosec G304 -- the profile's own Claude settings file
	if err != nil {
		return ""
	}
	var settings struct {
		APIKeyHelper string `json:"apiKeyHelper"`
	}
	if json.Unmarshal(data, &settings) != nil || settings.APIKeyHelper == "" {
		return ""
	}
	return fmt.Sprintf("%s sets apiKeyHelper: Claude Code uses the key it returns ahead of "+
		"this profile's native login, and aide cannot scrub it", path)
}

// launchConfigDir is the native config dir a launch of harnessName
// under ctx uses: the profile's own dir when it selects one, else the
// harness's dir variable from the profile's env:, then from the
// inherited environment (getenv), then from the harness's sandbox
// module when sandboxing is enabled, else its default dir. "" for a
// harness without a config dir variable.
func launchConfigDir(harnessName string, ctx config.Context, home string, getenv func(string) string) string {
	return ctx.LaunchConfigDir(harnessName, home, getenv)
}
