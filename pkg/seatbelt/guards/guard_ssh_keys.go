// SSH keys guard for macOS Seatbelt profiles.
//
// Protects private SSH keys by scanning ~/.ssh and denying access to
// files that are not on the safe-file allowlist. Also denies access to
// SSH agent sockets (SSH_AUTH_SOCK, GPG agent SSH bridge, and standard
// /tmp/ssh-* patterns) to prevent authentication via agent delegation.
// Uses the correct allow-broad/deny-narrow pattern.

package guards

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

var sshSafeFiles = map[string]bool{
	"known_hosts":     true,
	"known_hosts.old": true,
	"config":          true,
	"authorized_keys": true,
	"environment":     true,
}

type sshKeysGuard struct{}

// SSHKeysGuard returns a Guard that denies access to SSH private keys
// while allowing known_hosts and config reads.
func SSHKeysGuard() seatbelt.Guard { return &sshKeysGuard{} }

func (g *sshKeysGuard) Name() string        { return "ssh-keys" }
func (g *sshKeysGuard) Type() string        { return "default" }
func (g *sshKeysGuard) Description() string {
	return "Blocks access to SSH private keys and agent sockets; allows known_hosts and config"
}

func (g *sshKeysGuard) Rules(ctx *seatbelt.Context) seatbelt.GuardResult {
	result := seatbelt.GuardResult{}
	sshDir := ctx.HomePath(".ssh")

	// Agent socket denial applies regardless of whether ~/.ssh exists.
	// SSH authentication can happen via agent sockets alone.
	result.Rules = append(result.Rules, sshAgentSocketDenyRules(ctx)...)

	if !dirExists(sshDir) {
		result.Skipped = append(result.Skipped,
			fmt.Sprintf("%s not found, SSH key protection skipped", sshDir))
		return result
	}

	entries, err := os.ReadDir(sshDir)
	if err != nil {
		result.Skipped = append(result.Skipped,
			fmt.Sprintf("%s unreadable, SSH key protection skipped", sshDir))
		return result
	}

	// Safe files (known_hosts, config, *.pub) are now readable via the
	// filesystem guard's ~/.ssh subpath allow. We only need deny rules
	// for private keys.
	var denyRules []seatbelt.Rule

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		fullPath := filepath.Join(sshDir, name)

		if isSafeSSHFile(name) {
			result.Allowed = append(result.Allowed, fullPath)
		} else {
			denyRules = append(denyRules, DenyFile(fullPath)...)
			result.Protected = append(result.Protected, fullPath)
		}
	}

	if len(denyRules) > 0 {
		result.Rules = append(result.Rules, seatbelt.SectionDeny("SSH private keys (deny)"))
		result.Rules = append(result.Rules, denyRules...)
	}

	// Metadata for ~/.ssh directory traversal
	result.Rules = append(result.Rules,
		seatbelt.AllowRule(fmt.Sprintf(`(allow file-read-metadata (literal "%s"))`, sshDir)))

	return result
}

// sshAgentSocketDenyRules returns deny rules for SSH agent sockets.
// Uses network-outbound with unix-socket filtering (not file-level deny)
// because SSH connects to agent sockets via connect(), which is governed
// by the network-outbound operation in seatbelt, not file-read/write.
//
// Denies:
// 1. The socket path in SSH_AUTH_SOCK (if set)
// 2. The GPG agent SSH socket at ~/.gnupg/S.gpg-agent.ssh (if it exists)
// 3. Standard ssh-agent socket patterns in /tmp and /private/tmp
func sshAgentSocketDenyRules(ctx *seatbelt.Context) []seatbelt.Rule {
	var rules []seatbelt.Rule
	denied := make(map[string]bool)

	// 1. Deny network-outbound to the SSH_AUTH_SOCK socket path
	if sock, ok := ctx.EnvLookup("SSH_AUTH_SOCK"); ok && sock != "" {
		rules = append(rules, seatbelt.SectionDeny("SSH agent socket (SSH_AUTH_SOCK)"))
		rules = append(rules, denyUnixSocket(sock))
		denied[sock] = true
	}

	// 2. Deny the GPG agent SSH socket if it exists
	gpgAgentSock := ctx.HomePath(".gnupg/S.gpg-agent.ssh")
	if pathExists(gpgAgentSock) && !denied[gpgAgentSock] {
		rules = append(rules, seatbelt.SectionDeny("GPG agent SSH socket"))
		rules = append(rules, denyUnixSocket(gpgAgentSock))
	}

	// 3. Deny standard ssh-agent socket patterns in /tmp
	rules = append(rules, seatbelt.SectionDeny("Standard ssh-agent sockets"))
	rules = append(rules,
		seatbelt.DenyRule(`(deny network-outbound (remote unix-socket (path-regex #"^/tmp/ssh-[^/]+/agent\.\d+$")))`),
		seatbelt.DenyRule(`(deny network-outbound (remote unix-socket (path-regex #"^/private/tmp/ssh-[^/]+/agent\.\d+$")))`),
	)

	return rules
}

// denyUnixSocket denies network-outbound to a specific unix socket path.
func denyUnixSocket(path string) seatbelt.Rule {
	return seatbelt.DenyRule(fmt.Sprintf(`(deny network-outbound (remote unix-socket (path-literal "%s")))`, path))
}

func isSafeSSHFile(name string) bool {
	if sshSafeFiles[name] {
		return true
	}
	return strings.HasSuffix(name, ".pub")
}
