// SSH keys guard for macOS Seatbelt profiles.
//
// Protects private SSH keys from read and write access while allowing
// known_hosts and config files which are generally safe to read.

package guards

import "github.com/jskswamy/aide/pkg/seatbelt"

type sshKeysGuard struct{}

// SSHKeysGuard returns a Guard that denies access to SSH private keys
// while allowing known_hosts and config reads.
func SSHKeysGuard() seatbelt.Guard { return &sshKeysGuard{} }

func (g *sshKeysGuard) Name() string        { return "ssh-keys" }
func (g *sshKeysGuard) Type() string        { return "default" }
func (g *sshKeysGuard) Description() string {
	return "Blocks access to SSH private keys; allows known_hosts and config"
}

func (g *sshKeysGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	home := ctx.HomeDir

	return []seatbelt.Rule{
		// Deny all reads/writes to .ssh via subpath — catches private keys
		seatbelt.SectionRestrict("SSH keys (deny)"),
		seatbelt.RestrictRule(`(deny file-read-data
    ` + seatbelt.HomeSubpath(home, ".ssh") + `
)`),
		seatbelt.RestrictRule(`(deny file-write*
    ` + seatbelt.HomeSubpath(home, ".ssh") + `
)`),

		// Allow known_hosts and config via literal — beats subpath deny
		seatbelt.SectionGrant("SSH known_hosts and config (allow)"),
		seatbelt.GrantRule(`(allow file-read*
    ` + seatbelt.HomeLiteral(home, ".ssh/known_hosts") + `
    ` + seatbelt.HomeLiteral(home, ".ssh/config") + `
)`),

		// Allow directory listing of .ssh (metadata only)
		seatbelt.SectionGrant("SSH directory listing (metadata)"),
		seatbelt.GrantRule(`(allow file-read-metadata
    ` + seatbelt.HomeLiteral(home, ".ssh") + `
)`),
	}
}
