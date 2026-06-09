package guards

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

// sshURLPortRe matches the explicit port in ssh://[user@]host:PORT/...
var sshURLPortRe = regexp.MustCompile(`ssh://[^@\s]+@?[^:/\s]+:(\d{1,5})\b`)

// sshConfigPortRe matches "Port N" lines in ~/.ssh/config (case-insensitive,
// leading whitespace allowed). One match per line.
var sshConfigPortRe = regexp.MustCompile(`(?im)^[ \t]*Port[ \t]+(\d{1,5})[ \t]*$`)

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
	// Linux Landlock equivalent of the read-only grant above. If another guard
	// (e.g. dev-credentials) denies ~/.ssh, deny-wins in DeriveGrantedPathSet
	// strips it again — so this never widens beyond intent.
	result.Readable = append(result.Readable, filepath.Join(home, ".ssh"))

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
	ports, skipNotes := resolveSSHPorts(ctx)
	result.Skipped = append(result.Skipped, skipNotes...)
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
//
//	A. ~/.ssh/config Host/Port directives
//	B. .git/config ssh:// URLs with explicit ports
//	C. AIDE_SSH_PORTS env var (comma-separated)
//	D. ctx.SSHPorts (set from .aide.yaml capabilities.ssh.ports)
//
// Falls back to [22] if no port is declared anywhere. Returns sorted-deduped
// ports plus any human-readable notes about ignored input.
func resolveSSHPorts(ctx *seatbelt.Context) ([]int, []string) {
	set := make(map[int]struct{})
	var notes []string

	// Channel D (explicit): .aide.yaml capabilities.ssh.ports → ctx.SSHPorts.
	for _, p := range ctx.SSHPorts {
		if p >= 1 && p <= 65535 {
			set[p] = struct{}{}
		}
	}

	// Channel A (auto-detect): ~/.ssh/config Port directives. Presence of an
	// ssh config implies SSH usage, so include the :22 default for hosts
	// without an explicit Port line.
	if home := ctx.HomeDir; home != "" {
		if data, err := os.ReadFile(filepath.Join(home, ".ssh", "config")); err == nil {
			set[22] = struct{}{}
			for _, m := range sshConfigPortRe.FindAllStringSubmatch(string(data), -1) {
				if n, err := strconv.Atoi(m[1]); err == nil && n >= 1 && n <= 65535 {
					set[n] = struct{}{}
				}
			}
		}
	}

	// Channel B (auto-detect): ssh:// URLs in <project>/.git/config.
	// When any git remote exists, scp-style (git@host:path) uses port 22
	// implicitly, so add :22 alongside any explicit ssh:// ports detected.
	if ctx.ProjectRoot != "" {
		gitConfigPath := filepath.Join(ctx.ProjectRoot, ".git", "config")
		if data, err := os.ReadFile(gitConfigPath); err == nil {
			text := string(data)
			if strings.Contains(text, "[remote ") {
				set[22] = struct{}{}
			}
			for _, m := range sshURLPortRe.FindAllStringSubmatch(text, -1) {
				if n, err := strconv.Atoi(m[1]); err == nil && n >= 1 && n <= 65535 {
					set[n] = struct{}{}
				}
			}
		}
	}

	// Channel C (explicit): AIDE_SSH_PORTS env override. Pure declaration —
	// only the listed ports, no implicit :22 unless the user includes it.
	if raw, ok := ctx.EnvLookup("AIDE_SSH_PORTS"); ok && raw != "" {
		envSet := make(map[int]struct{})
		var ignored []string
		for _, tok := range strings.Split(raw, ",") {
			tok = strings.TrimSpace(tok)
			if tok == "" {
				continue
			}
			n, err := strconv.Atoi(tok)
			if err != nil || n < 1 || n > 65535 {
				ignored = append(ignored, tok)
				continue
			}
			envSet[n] = struct{}{}
		}
		if len(ignored) > 0 {
			notes = append(notes,
				fmt.Sprintf("AIDE_SSH_PORTS: ignored invalid entries %s", strings.Join(ignored, ",")))
		}
		// Explicit env replaces auto-detected set (deny-by-default discipline:
		// if you declare ports, you declare all of them).
		if len(envSet) > 0 {
			set = envSet
		}
	}

	if len(set) == 0 {
		return []int{22}, notes
	}

	out := make([]int, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sort.Ints(out)
	return out, notes
}

