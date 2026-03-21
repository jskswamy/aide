// Keychain integration module for macOS Seatbelt profiles.
//
// Rules ported from agent-safehouse by Eugene Goldin:
// https://github.com/eugene1g/agent-safehouse
// Source: profiles/55-integrations-optional/keychain.sb

package modules

import "github.com/jskswamy/aide/pkg/seatbelt"

type keychainIntegrationModule struct{}

// KeychainIntegration returns a module with macOS Keychain sandbox rules.
func KeychainIntegration() seatbelt.Module { return &keychainIntegrationModule{} }

func (m *keychainIntegrationModule) Name() string { return "Keychain Integration" }

func (m *keychainIntegrationModule) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	home := ctx.HomeDir

	return []seatbelt.Rule{
		// User keychain (read-write)
		seatbelt.Section("User keychain"),
		seatbelt.Raw(`(allow file-read* file-write*
    ` + seatbelt.HomeSubpath(home, "Library/Keychains") + `
    ` + seatbelt.HomeLiteral(home, "Library/Preferences/com.apple.security.plist") + `
)`),

		// System keychain (read-only)
		seatbelt.Section("System keychain"),
		seatbelt.Raw(`(allow file-read*
    (literal "/Library/Preferences/com.apple.security.plist")
    (literal "/Library/Keychains/System.keychain")
    (subpath "/private/var/db/mds")
)`),

		// Keychain metadata traversal
		seatbelt.Section("Keychain metadata traversal"),
		seatbelt.Raw(`(allow file-read-metadata
    ` + seatbelt.HomeLiteral(home, "Library") + `
    ` + seatbelt.HomeLiteral(home, "Library/Keychains") + `
    (literal "/Library")
    (literal "/Library/Keychains")
)`),

		// Security Mach services
		seatbelt.Section("Security Mach services"),
		seatbelt.Raw(`(allow mach-lookup
    (global-name "com.apple.SecurityServer")
    (global-name "com.apple.security.agent")
    (global-name "com.apple.securityd.xpc")
    (global-name "com.apple.security.authhost")
    (global-name "com.apple.secd")
    (global-name "com.apple.trustd")
)`),

		// Security IPC shared memory
		seatbelt.Section("Security IPC shared memory"),
		seatbelt.Raw(`(allow ipc-posix-shm-read-data ipc-posix-shm-write-create ipc-posix-shm-write-data
    (ipc-posix-name "com.apple.AppleDatabaseChanged")
)`),
	}
}
