// SSH keys guard for macOS Seatbelt profiles.
//
// Protects private SSH keys by scanning ~/.ssh and denying access to
// files that are not on the safe-file allowlist. Uses the correct
// allow-broad/deny-narrow pattern: allow known-safe files, deny
// everything else individually.

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
	return "Blocks access to SSH private keys; allows known_hosts and config"
}

func (g *sshKeysGuard) Rules(ctx *seatbelt.Context) seatbelt.GuardResult {
	result := seatbelt.GuardResult{}
	sshDir := ctx.HomePath(".ssh")

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

func isSafeSSHFile(name string) bool {
	if sshSafeFiles[name] {
		return true
	}
	return strings.HasSuffix(name, ".pub")
}
