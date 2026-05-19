package guards

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

// sshRemoteRe matches ssh:// or git@host: style URLs in a git config remote.url line.
var sshRemoteRe = regexp.MustCompile(`(?m)^\s*url\s*=\s*(ssh://|[^@\s]+@[^:\s]+:)`)

type gitRemoteGuard struct{}

// GitRemoteGuard returns an opt-in guard for git remote operations over HTTPS.
// SSH-based git transport is gated separately via the 'ssh' capability.
func GitRemoteGuard() seatbelt.Guard { return &gitRemoteGuard{} }

func (g *gitRemoteGuard) Name() string { return "git-remote" }
func (g *gitRemoteGuard) Type() string { return "opt-in" }
func (g *gitRemoteGuard) Description() string {
	return "Git remote operations over HTTPS (port 443). For SSH-based remotes, also enable 'ssh'."
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

	// Git credential manager config (read-only, if present)
	gcmDir := filepath.Join(home, ".config", "git-credential-manager")
	if dirExists(gcmDir) {
		result.Rules = append(result.Rules,
			seatbelt.SectionAllow("Git Credential Manager config (read-only)"),
			seatbelt.AllowRule(fmt.Sprintf("(allow file-read* %s)",
				seatbelt.HomeSubpath(home, ".config/git-credential-manager"))),
		)
	}

	// Network outbound on HTTPS
	result.Rules = append(result.Rules,
		seatbelt.SectionAllow("Network outbound for git transport over HTTPS (port 443)"),
		seatbelt.AllowRule("(allow network-outbound\n    (remote tcp \"*:443\")\n)"),
	)

	// Defense-in-depth: deny ~/.git-credentials
	gitCredentials := filepath.Join(home, ".git-credentials")
	result.Rules = append(result.Rules,
		seatbelt.SectionDeny("Plaintext git credentials (defense-in-depth)"),
		seatbelt.DenyLiteral(gitCredentials, "file-read-data"),
		seatbelt.DenyLiteral(gitCredentials, "file-write*"),
	)
	result.Protected = append(result.Protected, gitCredentials)

	// Discoverability hint: if the project has SSH-style remotes, point the
	// user at the 'ssh' capability — this guard only covers HTTPS.
	if ctx.ProjectRoot != "" {
		gitConfigPath := filepath.Join(ctx.ProjectRoot, ".git", "config")
		if data, err := os.ReadFile(gitConfigPath); err == nil {
			if sshRemoteRe.Match(data) {
				result.Hints = append(result.Hints,
					"git-remote: detected SSH remote(s) in .git/config; enable the 'ssh' capability to push/fetch over SSH (aide cap enable ssh)")
			}
		}
	}

	return result
}
