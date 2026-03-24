// Browsers guard for macOS Seatbelt profiles.
//
// Protects browser profile directories (cookies, passwords, history)
// from being read or written by sandboxed processes.

package guards

import (
	"fmt"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

type browsersGuard struct{}

// BrowsersGuard returns a Guard that denies access to browser profile directories.
func BrowsersGuard() seatbelt.Guard { return &browsersGuard{} }

func (g *browsersGuard) Name() string        { return "browsers" }
func (g *browsersGuard) Type() string        { return "default" }
func (g *browsersGuard) Description() string {
	return "Blocks access to browser data (cookies, passwords, history)"
}

func (g *browsersGuard) Rules(ctx *seatbelt.Context) seatbelt.GuardResult {
	result := seatbelt.GuardResult{}

	switch ctx.GOOS {
	case "linux":
		g.linuxPaths(ctx, &result)
	default:
		// darwin and unknown default to darwin paths
		g.darwinPaths(ctx, &result)
	}

	if len(result.Rules) > 0 {
		result.Rules = append([]seatbelt.Rule{seatbelt.SectionDeny("Browser profiles")}, result.Rules...)
	}

	return result
}

func (g *browsersGuard) darwinPaths(ctx *seatbelt.Context, result *seatbelt.GuardResult) {
	appSupport := ctx.HomePath("Library/Application Support")

	browsers := []string{
		"Google/Chrome",
		"Google/Chrome Canary",
		"Firefox",
		"Safari",
		"BraveSoftware/Brave-Browser",
		"Microsoft Edge",
		"Arc",
		"Chromium",
	}

	for _, b := range browsers {
		path := appSupport + "/" + b
		if dirExists(path) {
			result.Rules = append(result.Rules, DenyDir(path)...)
			result.Protected = append(result.Protected, path)
		} else {
			result.Skipped = append(result.Skipped, fmt.Sprintf("%s not found", path))
		}
	}
}

func (g *browsersGuard) linuxPaths(ctx *seatbelt.Context, result *seatbelt.GuardResult) {
	configDir := ctx.HomePath(".config")
	mozillaDir := ctx.HomePath(".mozilla")
	snapDir := ctx.HomePath("snap")

	browsers := []struct {
		base string
		name string
	}{
		{configDir, "google-chrome"},
		{configDir, "google-chrome-beta"},
		{mozillaDir, "firefox"},
		{configDir, "BraveSoftware/Brave-Browser"},
		{configDir, "microsoft-edge"},
		{configDir, "chromium"},
		{snapDir, "chromium"},
	}

	for _, b := range browsers {
		path := b.base + "/" + b.name
		if dirExists(path) {
			result.Rules = append(result.Rules, DenyDir(path)...)
			result.Protected = append(result.Protected, path)
		} else {
			result.Skipped = append(result.Skipped, fmt.Sprintf("%s not found", path))
		}
	}
}
