// Sensitive opt-in guards for macOS Seatbelt profiles.
//
// These guards are opt-in because they protect credentials that some tools
// legitimately need to access (e.g. docker daemon, GitHub CLI auth).

package guards

import (
	"fmt"
	"path/filepath"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

// --- docker ---

type dockerGuard struct{}

// DockerGuard returns an opt-in Guard that denies access to Docker credentials.
func DockerGuard() seatbelt.Guard { return &dockerGuard{} }

func (g *dockerGuard) Name() string        { return "docker" }
func (g *dockerGuard) Type() string        { return "default" }
func (g *dockerGuard) Description() string { return "Blocks access to Docker registry credentials" }

func (g *dockerGuard) Rules(ctx *seatbelt.Context) seatbelt.GuardResult {
	result := seatbelt.GuardResult{}

	// DOCKER_CONFIG points to the directory; config.json is always inside it.
	var configDir string
	if v, ok := ctx.EnvLookup("DOCKER_CONFIG"); ok && v != "" {
		configDir = v
		result.Overrides = append(result.Overrides, seatbelt.Override{
			EnvVar:      "DOCKER_CONFIG",
			Value:       v,
			DefaultPath: ctx.HomePath(".docker"),
		})
	} else {
		configDir = ctx.HomePath(".docker")
	}
	configFile := filepath.Join(configDir, "config.json")

	if !pathExists(configFile) {
		result.Skipped = append(result.Skipped, fmt.Sprintf("%s not found", configFile))
		return result
	}

	result.Rules = append(result.Rules, seatbelt.SectionDeny("Docker credentials"))
	result.Rules = append(result.Rules, DenyFile(configFile)...)
	result.Protected = append(result.Protected, configFile)
	return result
}

// --- github-cli ---

type githubCLIGuard struct{}

// GithubCLIGuard returns an opt-in Guard that denies access to GitHub CLI credentials.
func GithubCLIGuard() seatbelt.Guard { return &githubCLIGuard{} }

func (g *githubCLIGuard) Name() string        { return "github-cli" }
func (g *githubCLIGuard) Type() string        { return "default" }
func (g *githubCLIGuard) Description() string { return "Blocks access to GitHub CLI credentials" }

func (g *githubCLIGuard) Rules(ctx *seatbelt.Context) seatbelt.GuardResult {
	result := seatbelt.GuardResult{}
	ghDir := ctx.HomePath(".config/gh")

	if !dirExists(ghDir) {
		result.Skipped = append(result.Skipped, fmt.Sprintf("%s not found", ghDir))
		return result
	}

	result.Rules = append(result.Rules, seatbelt.SectionDeny("GitHub CLI credentials"))
	result.Rules = append(result.Rules, DenyDir(ghDir)...)
	result.Protected = append(result.Protected, ghDir)
	return result
}

// --- npm ---

type npmGuard struct{}

// NPMGuard returns an opt-in Guard that denies access to npm/yarn credentials.
func NPMGuard() seatbelt.Guard { return &npmGuard{} }

func (g *npmGuard) Name() string        { return "npm" }
func (g *npmGuard) Type() string        { return "default" }
func (g *npmGuard) Description() string { return "Blocks access to npm and yarn auth tokens" }

func (g *npmGuard) Rules(ctx *seatbelt.Context) seatbelt.GuardResult {
	result := seatbelt.GuardResult{}

	npmrc := ctx.HomePath(".npmrc")
	if pathExists(npmrc) {
		result.Rules = append(result.Rules, DenyFile(npmrc)...)
		result.Protected = append(result.Protected, npmrc)
	} else {
		result.Skipped = append(result.Skipped, fmt.Sprintf("%s not found", npmrc))
	}

	yarnrc := ctx.HomePath(".yarnrc")
	if pathExists(yarnrc) {
		result.Rules = append(result.Rules, DenyFile(yarnrc)...)
		result.Protected = append(result.Protected, yarnrc)
	} else {
		result.Skipped = append(result.Skipped, fmt.Sprintf("%s not found", yarnrc))
	}

	if len(result.Rules) > 0 {
		result.Rules = append([]seatbelt.Rule{seatbelt.SectionDeny("npm/yarn credentials")}, result.Rules...)
	}

	return result
}

// --- netrc ---

type netrcGuard struct{}

// NetrcGuard returns an opt-in Guard that denies access to ~/.netrc.
func NetrcGuard() seatbelt.Guard { return &netrcGuard{} }

func (g *netrcGuard) Name() string        { return "netrc" }
func (g *netrcGuard) Type() string        { return "default" }
func (g *netrcGuard) Description() string { return "Blocks access to netrc credentials" }

func (g *netrcGuard) Rules(ctx *seatbelt.Context) seatbelt.GuardResult {
	result := seatbelt.GuardResult{}
	netrcPath := ctx.HomePath(".netrc")

	if !pathExists(netrcPath) {
		result.Skipped = append(result.Skipped, fmt.Sprintf("%s not found", netrcPath))
		return result
	}

	result.Rules = append(result.Rules, seatbelt.SectionDeny("netrc credentials"))
	result.Rules = append(result.Rules, DenyFile(netrcPath)...)
	result.Protected = append(result.Protected, netrcPath)
	return result
}

// --- vercel ---

type vercelGuard struct{}

// VercelGuard returns an opt-in Guard that denies access to Vercel credentials.
func VercelGuard() seatbelt.Guard { return &vercelGuard{} }

func (g *vercelGuard) Name() string        { return "vercel" }
func (g *vercelGuard) Type() string        { return "opt-in" }
func (g *vercelGuard) Description() string { return "Blocks access to Vercel CLI credentials" }

func (g *vercelGuard) Rules(ctx *seatbelt.Context) seatbelt.GuardResult {
	result := seatbelt.GuardResult{}
	vercelDir := ctx.HomePath(".config/vercel")

	if !dirExists(vercelDir) {
		result.Skipped = append(result.Skipped, fmt.Sprintf("%s not found", vercelDir))
		return result
	}

	result.Rules = append(result.Rules, seatbelt.SectionDeny("Vercel credentials"))
	result.Rules = append(result.Rules, DenyDir(vercelDir)...)
	result.Protected = append(result.Protected, vercelDir)
	return result
}
