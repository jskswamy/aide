// Keychain integration guard for macOS Seatbelt profiles.
//
// Rules ported from agent-safehouse by Eugene Goldin:
// https://github.com/eugene1g/agent-safehouse
// Source: profiles/55-integrations-optional/keychain.sb

package guards

import "github.com/jskswamy/aide/pkg/seatbelt"

type keychainGuard struct{}

// KeychainGuard returns a Guard with macOS Keychain sandbox rules.
func KeychainGuard() seatbelt.Guard { return &keychainGuard{} }

func (g *keychainGuard) Name() string        { return "keychain" }
func (g *keychainGuard) Type() string        { return "always" }
func (g *keychainGuard) Description() string {
	return "macOS Keychain access for authentication and certificates"
}

func (g *keychainGuard) Rules(ctx *seatbelt.Context) seatbelt.GuardResult {
	home := ctx.HomeDir

	return seatbelt.GuardResult{Rules: []seatbelt.Rule{
		// User keychain (read-write)
		seatbelt.SectionAllow("User keychain"),
		seatbelt.AllowRule(`(allow file-read* file-write*
    ` + seatbelt.HomeSubpath(home, "Library/Keychains") + `
    ` + seatbelt.HomeLiteral(home, "Library/Preferences/com.apple.security.plist") + `
)`),

		// System keychain (read-only)
		seatbelt.SectionAllow("System keychain"),
		seatbelt.AllowRule(`(allow file-read*
    (literal "/Library/Preferences/com.apple.security.plist")
    (literal "/Library/Keychains/System.keychain")
    (subpath "/private/var/db/mds")
)`),

		// Keychain metadata traversal
		seatbelt.SectionAllow("Keychain metadata traversal"),
		seatbelt.AllowRule(`(allow file-read-metadata
    ` + seatbelt.HomeLiteral(home, "Library") + `
    ` + seatbelt.HomeLiteral(home, "Library/Keychains") + `
    (literal "/Library")
    (literal "/Library/Keychains")
)`),

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
