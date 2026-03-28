package guards

import (
	"fmt"
	"strings"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

type gitIntegrationGuard struct{}

func GitIntegrationGuard() seatbelt.Guard { return &gitIntegrationGuard{} }

func (g *gitIntegrationGuard) Name() string { return "git-integration" }
func (g *gitIntegrationGuard) Type() string { return "always" }
func (g *gitIntegrationGuard) Description() string {
	return "Git configuration files (read-only, parsed from gitconfig)"
}

func (g *gitIntegrationGuard) Rules(ctx *seatbelt.Context) seatbelt.GuardResult {
	if ctx == nil {
		return seatbelt.GuardResult{}
	}
	home := ctx.HomeDir
	if home == "" {
		return seatbelt.GuardResult{}
	}

	gcResult := ParseGitConfigWithEnv(home, ctx.ProjectRoot, ctx.EnvLookup)

	var result seatbelt.GuardResult

	if val, ok := ctx.EnvLookup("GIT_CONFIG_GLOBAL"); ok && val != "" {
		result.Overrides = append(result.Overrides, seatbelt.Override{
			EnvVar: "GIT_CONFIG_GLOBAL", Value: val, DefaultPath: home + "/.gitconfig",
		})
	}
	if val, ok := ctx.EnvLookup("GIT_CONFIG_SYSTEM"); ok && val != "" {
		result.Overrides = append(result.Overrides, seatbelt.Override{
			EnvVar: "GIT_CONFIG_SYSTEM", Value: val, DefaultPath: "/etc/gitconfig",
		})
	}

	allPaths := gcResult.AllPaths()
	if len(allPaths) == 0 {
		return result
	}

	var pathExprs []string
	for _, p := range allPaths {
		pathExprs = append(pathExprs, fmt.Sprintf(`    (literal "%s")`, p))
	}

	result.Rules = append(result.Rules,
		seatbelt.SectionAllow("Git configuration (read-only)"),
		seatbelt.AllowRule(fmt.Sprintf("(allow file-read*\n%s)", strings.Join(pathExprs, "\n"))),
	)

	// GPG signing support: if commit.gpgsign is enabled in any config,
	// allow access to ~/.gnupg for signing commits. GPG needs read+write
	// (trustdb lock files, random_seed) and agent socket access.
	if gcResult.GPGSign {
		gnupgHome := home + "/.gnupg"
		if val, ok := ctx.EnvLookup("GNUPGHOME"); ok && val != "" {
			gnupgHome = val
		}
		result.Rules = append(result.Rules,
			seatbelt.SectionAllow("GPG signing for git commits (read-write)"),
			seatbelt.AllowRule(fmt.Sprintf("(allow file-read* file-write*\n    %s)",
				seatbelt.HomeSubpath(home, ".gnupg"))),
		)
		// GPG agent unix socket for passphrase caching
		result.Rules = append(result.Rules,
			seatbelt.SectionAllow("GPG agent socket"),
			seatbelt.AllowRule(fmt.Sprintf(`(allow network-outbound
    (remote unix-socket (path-literal "%s/S.gpg-agent")))`, gnupgHome)),
		)
	}

	return result
}
