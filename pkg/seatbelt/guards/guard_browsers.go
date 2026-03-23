// Browsers guard for macOS Seatbelt profiles.
//
// Protects browser profile directories (cookies, passwords, history)
// from being read or written by sandboxed processes.

package guards

import "github.com/jskswamy/aide/pkg/seatbelt"

type browsersGuard struct{}

// BrowsersGuard returns a Guard that denies access to browser profile directories.
func BrowsersGuard() seatbelt.Guard { return &browsersGuard{} }

func (g *browsersGuard) Name() string        { return "browsers" }
func (g *browsersGuard) Type() string        { return "default" }
func (g *browsersGuard) Description() string { return "Browser profile directories (cookies, passwords, history)" }

func (g *browsersGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	var rules []seatbelt.Rule
	rules = append(rules, seatbelt.Section("Browser profiles"))

	switch ctx.GOOS {
	case "linux":
		rules = append(rules, g.linuxRules(ctx)...)
	default:
		// darwin and unknown default to darwin paths
		rules = append(rules, g.darwinRules(ctx)...)
	}
	return rules
}

func (g *browsersGuard) darwinRules(ctx *seatbelt.Context) []seatbelt.Rule {
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

	var rules []seatbelt.Rule
	for _, b := range browsers {
		p := appSupport + "/" + b
		rules = append(rules,
			seatbelt.Raw(`(deny file-read-data
    `+`(subpath "`+p+`")`+`
)`),
			seatbelt.Raw(`(deny file-write*
    `+`(subpath "`+p+`")`+`
)`),
		)
	}
	return rules
}

func (g *browsersGuard) linuxRules(ctx *seatbelt.Context) []seatbelt.Rule {
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

	var rules []seatbelt.Rule
	for _, b := range browsers {
		p := b.base + "/" + b.name
		rules = append(rules,
			seatbelt.Raw(`(deny file-read-data
    `+`(subpath "`+p+`")`+`
)`),
			seatbelt.Raw(`(deny file-write*
    `+`(subpath "`+p+`")`+`
)`),
		)
	}
	return rules
}
