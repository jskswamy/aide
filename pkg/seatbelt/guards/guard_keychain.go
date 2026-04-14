// Keychain integration guard for macOS Seatbelt profiles.
//
// Rules ported from agent-safehouse by Eugene Goldin:
// https://github.com/eugene1g/agent-safehouse
// Source: profiles/55-integrations-optional/keychain.sb

package guards

import (
	"fmt"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

type keychainGuard struct{}

// KeychainGuard returns a Guard with macOS Keychain sandbox rules.
func KeychainGuard() seatbelt.Guard { return &keychainGuard{} }

func (g *keychainGuard) Name() string        { return "keychain" }
func (g *keychainGuard) Type() string        { return "always" }
func (g *keychainGuard) Description() string {
	return "macOS Keychain access for authentication and certificates"
}

func (g *keychainGuard) Rules(ctx *seatbelt.Context) seatbelt.GuardResult {
	if ctx == nil || ctx.HomeDir == "" {
		return seatbelt.GuardResult{}
	}
	home := ctx.HomeDir

	return seatbelt.GuardResult{Rules: []seatbelt.Rule{
		seatbelt.SectionAllow("User keychain (read)"),
		seatbelt.AllowRule(fmt.Sprintf(`(allow file-read*
    %s
    %s
)`, seatbelt.HomeSubpath(home, "Library/Keychains"),
			seatbelt.HomeLiteral(home, "Library/Preferences/com.apple.security.plist"))),

		// Write access to user keychain for credential storage (OAuth token refresh).
		// Subpath required: securityd creates .sb-* temp files and SQLite WAL/SHM
		// alongside login.keychain-db. API-level isolation (securityd ACLs) prevents
		// cross-app credential access even with file-write.
		seatbelt.SectionAllow("User keychain (write)"),
		seatbelt.AllowRule(fmt.Sprintf(`(allow file-write*
    %s
)`, seatbelt.HomeSubpath(home, "Library/Keychains"))),

		// System keychain reads and metadata traversal are now covered
		// by the system-runtime guard's broad /Library and /private reads.

		// Security Mach services
		seatbelt.SectionAllow("Security Mach services"),
		seatbelt.AllowRule(`(allow mach-lookup
    (global-name "com.apple.SecurityServer")
    (global-name "com.apple.security.agent")
    (global-name "com.apple.securityd.xpc")
    (global-name "com.apple.security.authhost")
    (global-name "com.apple.secd")
    (global-name "com.apple.trustd")
)`),

		// Security IPC shared memory
		seatbelt.SectionAllow("Security IPC shared memory"),
		seatbelt.AllowRule(`(allow ipc-posix-shm-read-data ipc-posix-shm-write-create ipc-posix-shm-write-data
    (ipc-posix-name "com.apple.AppleDatabaseChanged")
)`),
	}}
}
