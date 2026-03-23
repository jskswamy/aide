// Sensitive opt-in guards for macOS Seatbelt profiles.
//
// These guards are opt-in because they protect credentials that some tools
// legitimately need to access (e.g. docker daemon, GitHub CLI auth).

package guards

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
func (g *dockerGuard) Description() string { return "Blocks access to Docker registry credentials" }

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
	rules = append(rules, seatbelt.SectionRestrict("Docker credentials"))
	rules = append(rules, DenyFile(configFile)...)
	return rules
}

// --- github-cli ---

type githubCLIGuard struct{}

// GithubCLIGuard returns an opt-in Guard that denies access to GitHub CLI credentials.
func GithubCLIGuard() seatbelt.Guard { return &githubCLIGuard{} }

func (g *githubCLIGuard) Name() string        { return "github-cli" }
func (g *githubCLIGuard) Type() string        { return "opt-in" }
func (g *githubCLIGuard) Description() string { return "Blocks access to GitHub CLI credentials" }

func (g *githubCLIGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	var rules []seatbelt.Rule
	rules = append(rules, seatbelt.SectionRestrict("GitHub CLI credentials"))
	rules = append(rules, DenyDir(ctx.HomePath(".config/gh"))...)
	return rules
}

// --- npm ---

type npmGuard struct{}

// NPMGuard returns an opt-in Guard that denies access to npm/yarn credentials.
func NPMGuard() seatbelt.Guard { return &npmGuard{} }

func (g *npmGuard) Name() string        { return "npm" }
func (g *npmGuard) Type() string        { return "opt-in" }
func (g *npmGuard) Description() string { return "Blocks access to npm and yarn auth tokens" }

func (g *npmGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	var rules []seatbelt.Rule
	rules = append(rules, seatbelt.SectionRestrict("npm/yarn credentials"))
	rules = append(rules, DenyFile(ctx.HomePath(".npmrc"))...)
	rules = append(rules, DenyFile(ctx.HomePath(".yarnrc"))...)
	return rules
}

// --- netrc ---

type netrcGuard struct{}

// NetrcGuard returns an opt-in Guard that denies access to ~/.netrc.
func NetrcGuard() seatbelt.Guard { return &netrcGuard{} }

func (g *netrcGuard) Name() string        { return "netrc" }
func (g *netrcGuard) Type() string        { return "opt-in" }
func (g *netrcGuard) Description() string { return "Blocks access to netrc credentials" }

func (g *netrcGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	var rules []seatbelt.Rule
	rules = append(rules, seatbelt.SectionRestrict("netrc credentials"))
	rules = append(rules, DenyFile(ctx.HomePath(".netrc"))...)
	return rules
}

// --- vercel ---

type vercelGuard struct{}

// VercelGuard returns an opt-in Guard that denies access to Vercel credentials.
func VercelGuard() seatbelt.Guard { return &vercelGuard{} }

func (g *vercelGuard) Name() string        { return "vercel" }
func (g *vercelGuard) Type() string        { return "opt-in" }
func (g *vercelGuard) Description() string { return "Blocks access to Vercel CLI credentials" }

func (g *vercelGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	var rules []seatbelt.Rule
	rules = append(rules, seatbelt.SectionRestrict("Vercel credentials"))
	rules = append(rules, DenyDir(ctx.HomePath(".config/vercel"))...)
	return rules
}
