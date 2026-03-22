// Sensitive opt-in guards for macOS Seatbelt profiles.
//
// These guards are opt-in because they protect credentials that some tools
// legitimately need to access (e.g. docker daemon, GitHub CLI auth).

package modules

import (
	"path/filepath"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

// --- docker ---

type dockerGuard struct{}

// DockerGuard returns an opt-in Guard that denies access to Docker credentials.
func DockerGuard() seatbelt.Guard { return &dockerGuard{} }

func (g *dockerGuard) Name() string        { return "docker" }
func (g *dockerGuard) Type() string        { return "opt-in" }
func (g *dockerGuard) Description() string { return "Docker config.json (registry credentials)" }

func (g *dockerGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	// DOCKER_CONFIG points to the directory; config.json is always inside it.
	var configDir string
	if v, ok := ctx.EnvLookup("DOCKER_CONFIG"); ok && v != "" {
		configDir = v
	} else {
		configDir = ctx.HomePath(".docker")
	}
	configFile := filepath.Join(configDir, "config.json")

	var rules []seatbelt.Rule
	rules = append(rules, seatbelt.Section("Docker credentials"))
	rules = append(rules, denyLiteralRuleForPath(configFile)...)
	return rules
}

// --- github-cli ---

type githubCLIGuard struct{}

// GithubCLIGuard returns an opt-in Guard that denies access to GitHub CLI credentials.
func GithubCLIGuard() seatbelt.Guard { return &githubCLIGuard{} }

func (g *githubCLIGuard) Name() string        { return "github-cli" }
func (g *githubCLIGuard) Type() string        { return "opt-in" }
func (g *githubCLIGuard) Description() string { return "GitHub CLI auth tokens (~/.config/gh)" }

func (g *githubCLIGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	var rules []seatbelt.Rule
	rules = append(rules, seatbelt.Section("GitHub CLI credentials"))
	rules = append(rules, denySubpathRuleForPath(ctx.HomePath(".config/gh"))...)
	return rules
}

// --- npm ---

type npmGuard struct{}

// NPMGuard returns an opt-in Guard that denies access to npm/yarn credentials.
func NPMGuard() seatbelt.Guard { return &npmGuard{} }

func (g *npmGuard) Name() string        { return "npm" }
func (g *npmGuard) Type() string        { return "opt-in" }
func (g *npmGuard) Description() string { return "npm and yarn registry credentials (~/.npmrc, ~/.yarnrc)" }

func (g *npmGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	var rules []seatbelt.Rule
	rules = append(rules, seatbelt.Section("npm/yarn credentials"))
	rules = append(rules, denyLiteralRuleForPath(ctx.HomePath(".npmrc"))...)
	rules = append(rules, denyLiteralRuleForPath(ctx.HomePath(".yarnrc"))...)
	return rules
}

// --- netrc ---

type netrcGuard struct{}

// NetrcGuard returns an opt-in Guard that denies access to ~/.netrc.
func NetrcGuard() seatbelt.Guard { return &netrcGuard{} }

func (g *netrcGuard) Name() string        { return "netrc" }
func (g *netrcGuard) Type() string        { return "opt-in" }
func (g *netrcGuard) Description() string { return "netrc credentials file (~/.netrc)" }

func (g *netrcGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	var rules []seatbelt.Rule
	rules = append(rules, seatbelt.Section("netrc credentials"))
	rules = append(rules, denyLiteralRuleForPath(ctx.HomePath(".netrc"))...)
	return rules
}

// --- vercel ---

type vercelGuard struct{}

// VercelGuard returns an opt-in Guard that denies access to Vercel credentials.
func VercelGuard() seatbelt.Guard { return &vercelGuard{} }

func (g *vercelGuard) Name() string        { return "vercel" }
func (g *vercelGuard) Type() string        { return "opt-in" }
func (g *vercelGuard) Description() string { return "Vercel CLI credentials (~/.config/vercel)" }

func (g *vercelGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	var rules []seatbelt.Rule
	rules = append(rules, seatbelt.Section("Vercel credentials"))
	rules = append(rules, denySubpathRuleForPath(ctx.HomePath(".config/vercel"))...)
	return rules
}
