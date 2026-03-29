package guards

import (
	"fmt"
	"path/filepath"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

type gitRemoteGuard struct{}

// GitRemoteGuard returns an opt-in guard for git remote operations (SSH, credentials, network).
func GitRemoteGuard() seatbelt.Guard { return &gitRemoteGuard{} }

func (g *gitRemoteGuard) Name() string { return "git-remote" }
func (g *gitRemoteGuard) Type() string { return "opt-in" }
func (g *gitRemoteGuard) Description() string {
	return "Git remote operations — SSH keys, credentials, and network (ports 22/443)"
}

func (g *gitRemoteGuard) Rules(ctx *seatbelt.Context) seatbelt.GuardResult {
	if ctx == nil {
		return seatbelt.GuardResult{}
	}
	home := ctx.HomeDir
	if home == "" {
		return seatbelt.GuardResult{}
	}

	var result seatbelt.GuardResult

	// SSH key and config access (read-only, subpath covers all)
	result.Rules = append(result.Rules,
		seatbelt.SectionAllow("SSH keys and config for git transport (read-only)"),
		seatbelt.AllowRule(fmt.Sprintf("(allow file-read*\n    %s\n)",
			seatbelt.HomeSubpath(home, ".ssh"))),
	)

	// SSH agent socket
	if sock, ok := ctx.EnvLookup("SSH_AUTH_SOCK"); ok && sock != "" {
		result.Rules = append(result.Rules,
			seatbelt.SectionAllow("SSH agent socket"),
			seatbelt.AllowRule(fmt.Sprintf("(allow network-outbound\n    (remote unix-socket (path-literal \"%s\"))\n)", sock)),
		)
		result.Overrides = append(result.Overrides, seatbelt.Override{
			EnvVar: "SSH_AUTH_SOCK", Value: sock,
		})
	} else {
		result.Skipped = append(result.Skipped,
			"SSH_AUTH_SOCK not set — SSH agent socket rule skipped")
	}

	// Git credential manager config (read-only, if present)
	gcmDir := filepath.Join(home, ".config", "git-credential-manager")
	if dirExists(gcmDir) {
		result.Rules = append(result.Rules,
			seatbelt.SectionAllow("Git Credential Manager config (read-only)"),
			seatbelt.AllowRule(fmt.Sprintf("(allow file-read* %s)",
				seatbelt.HomeSubpath(home, ".config/git-credential-manager"))),
		)
	}

	// Network outbound on git transport ports
	result.Rules = append(result.Rules,
		seatbelt.SectionAllow("Network outbound for git transport (ports 22, 443)"),
		seatbelt.AllowRule("(allow network-outbound\n    (remote tcp \"*:22\")\n    (remote tcp \"*:443\")\n)"),
	)

	// Defense-in-depth: deny ~/.git-credentials
	gitCredentials := filepath.Join(home, ".git-credentials")
	result.Rules = append(result.Rules,
		seatbelt.SectionDeny("Plaintext git credentials (defense-in-depth)"),
		seatbelt.DenyRule(fmt.Sprintf("(deny file-read-data (literal \"%s\"))", gitCredentials)),
		seatbelt.DenyRule(fmt.Sprintf("(deny file-write* (literal \"%s\"))", gitCredentials)),
	)
	result.Protected = append(result.Protected, gitCredentials)

	return result
}
