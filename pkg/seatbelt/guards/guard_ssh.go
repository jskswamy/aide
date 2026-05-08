package guards

import (
	"fmt"
	"strings"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

type sshGuard struct{}

// SSHGuard returns an opt-in guard for SSH access — keys, agent, and outbound
// SSH transport (port 22 + custom ports). Required for git-over-SSH, ssh
// login, scp/rsync.
func SSHGuard() seatbelt.Guard { return &sshGuard{} }

func (g *sshGuard) Name() string { return "ssh" }
func (g *sshGuard) Type() string { return "opt-in" }
func (g *sshGuard) Description() string {
	return "SSH keys, agent, and outbound SSH transport (port 22 + custom). Required for: git over SSH, ssh login, scp/rsync."
}

func (g *sshGuard) Rules(ctx *seatbelt.Context) seatbelt.GuardResult {
	if ctx == nil {
		return seatbelt.GuardResult{}
	}
	home := ctx.HomeDir
	if home == "" {
		return seatbelt.GuardResult{}
	}

	var result seatbelt.GuardResult

	// SSH keys and config (read-only)
	result.Rules = append(result.Rules,
		seatbelt.SectionAllow("SSH keys and config (read-only)"),
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

	// Network outbound on resolved SSH ports (default [22])
	ports := resolveSSHPorts(ctx)
	var portRules []string
	for _, p := range ports {
		portRules = append(portRules, fmt.Sprintf("    (remote tcp \"*:%d\")", p))
	}
	result.Rules = append(result.Rules,
		seatbelt.SectionAllow("Network outbound for SSH transport"),
		seatbelt.AllowRule(fmt.Sprintf("(allow network-outbound\n%s\n)", strings.Join(portRules, "\n"))),
	)

	return result
}

// resolveSSHPorts returns the union of SSH ports declared via:
//   A. ~/.ssh/config Host/Port directives
//   B. .git/config ssh:// URLs with explicit ports
//   C. AIDE_SSH_PORTS env var (comma-separated)
//   D. ctx.SSHPorts (set from .aide.yaml capabilities.ssh.ports)
//
// Falls back to [22] if no port is declared anywhere.
func resolveSSHPorts(ctx *seatbelt.Context) []int {
	// Skeleton: full A/B/C/D resolution arrives in subsequent TDD cycles.
	return []int{22}
}
