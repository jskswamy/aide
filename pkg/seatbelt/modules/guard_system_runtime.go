// System runtime guard for macOS Seatbelt profiles.
//
// Rules ported from agent-safehouse by Eugene Goldin:
// https://github.com/eugene1g/agent-safehouse
// Source: profiles/10-system-runtime.sb

package modules

import "github.com/jskswamy/aide/pkg/seatbelt"

type systemRuntimeGuard struct{}

// SystemRuntimeGuard returns a Guard with macOS system runtime rules.
func SystemRuntimeGuard() seatbelt.Guard { return &systemRuntimeGuard{} }

// SystemRuntime returns a module with macOS system runtime rules.
// Deprecated: use SystemRuntimeGuard instead.
func SystemRuntime() seatbelt.Module { return &systemRuntimeGuard{} }

func (g *systemRuntimeGuard) Name() string        { return "system-runtime" }
func (g *systemRuntimeGuard) Type() string        { return "always" }
func (g *systemRuntimeGuard) Description() string { return "macOS system runtime paths, devices, and Mach services" }

func (g *systemRuntimeGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	home := ctx.HomeDir

	return []seatbelt.Rule{
		// 1. System binary paths
		seatbelt.Section("System binary paths"),
		seatbelt.Raw(`(allow file-read*
    (subpath "/usr")
    (subpath "/bin")
    (subpath "/sbin")
    (subpath "/opt")
    (subpath "/System/Library")
    (subpath "/System/Volumes/Preboot")
    (subpath "/Library/Apple")
    (subpath "/Library/Frameworks")
    (subpath "/Library/Fonts")
    (subpath "/Library/Filesystems/NetFSPlugins")
    (subpath "/Library/Preferences/Logging")
    (literal "/Library/Preferences/.GlobalPreferences.plist")
    (literal "/Library/Preferences/com.apple.networkd.plist")
    (literal "/dev")
)`),

		// 2. Root filesystem traversal
		seatbelt.Section("Root filesystem traversal"),
		seatbelt.Raw(`(allow file-read-data
    (literal "/")
)`),

		// 3. Metadata traversal
		seatbelt.Section("Metadata traversal"),
		// Git requires stat() on parent directories up to / for its
		// safe.directory ownership check. /Users is needed on macOS
		// so git can walk /Users → /Users/<user> → ... → repo root.
		seatbelt.Raw(`(allow file-read-metadata
    (literal "/")
    (literal "/Users")
    (subpath "/System")
    (subpath "/private/var/run")
)`),

		// 3. Private/etc paths
		seatbelt.Section("Private/etc paths"),
		seatbelt.Raw(`(allow file-read*
    (literal "/private")
    (literal "/private/var")
    (subpath "/private/var/db/timezone")
    (literal "/private/var/select/sh")
    (literal "/private/var/select/developer_dir")
    (literal "/var/select/developer_dir")
    (literal "/private/var/db/xcode_select_link")
    (literal "/var/db/xcode_select_link")
    (literal "/private/etc/hosts")
    (literal "/private/etc/resolv.conf")
    (literal "/private/etc/services")
    (literal "/private/etc/protocols")
    (literal "/private/etc/shells")
    (subpath "/private/etc/ssl")
    (literal "/private/etc/localtime")
    (literal "/etc")
    (literal "/var")
)`),

		// 4. Home metadata traversal
		seatbelt.Section("Home metadata traversal"),
		seatbelt.Raw(`(allow file-read-metadata
    (literal "/home")
    (literal "/private/etc")
    (subpath "/dev")
    ` + seatbelt.HomeLiteral(home, ".config") + `
    ` + seatbelt.HomeLiteral(home, ".cache") + `
    ` + seatbelt.HomeLiteral(home, ".local") + `
    ` + seatbelt.HomeLiteral(home, ".local/share") + `
)`),

		// 5. User preferences
		seatbelt.Section("User preferences"),
		seatbelt.Raw(`(allow file-read*
    ` + seatbelt.HomePrefix(home, "Library/Preferences/.GlobalPreferences") + `
    ` + seatbelt.HomePrefix(home, "Library/Preferences/com.apple.GlobalPreferences") + `
    ` + seatbelt.HomeSubpath(home, "Library/Preferences/ByHost") + `
    ` + seatbelt.HomeLiteral(home, ".CFUserTextEncoding") + `
    ` + seatbelt.HomeLiteral(home, ".config") + `
    ` + seatbelt.HomeLiteral(home, ".cache") + `
    ` + seatbelt.HomeLiteral(home, ".local/bin") + `
)`),

		// 6. Process rules
		seatbelt.Section("Process rules"),
		seatbelt.Allow("process-exec"),
		seatbelt.Allow("process-fork"),
		seatbelt.Allow("sysctl-read"),
		seatbelt.Raw("(allow process-info* (target same-sandbox))"),
		seatbelt.Raw("(allow signal (target same-sandbox))"),
		seatbelt.Raw("(allow mach-priv-task-port (target same-sandbox))"),
		seatbelt.Allow("pseudo-tty"),

		// 7. Temp dirs
		seatbelt.Section("Temp dirs"),
		seatbelt.Raw(`(allow file-read* file-write*
    (subpath "/tmp")
    (subpath "/private/tmp")
    (subpath "/var/folders")
    (subpath "/private/var/folders")
)`),

		// 8. Launchd listener deny
		seatbelt.Section("Launchd listener deny"),
		seatbelt.Raw(`(deny file-read* file-write*
    (regex #"^/private/tmp/com\.apple\.launchd\.[^/]+/Listeners$")
    (regex #"^/tmp/com\.apple\.launchd\.[^/]+/Listeners$")
)`),

		// 9. Device nodes (read-write)
		seatbelt.Section("Device nodes"),
		seatbelt.Raw(`(allow file-read* file-write*
    (subpath "/dev/fd")
    (literal "/dev/stdout")
    (literal "/dev/stderr")
    (literal "/dev/null")
    (literal "/dev/tty")
    (literal "/dev/ptmx")
    (literal "/dev/dtracehelper")
    (regex #"^/dev/tty")
    (regex #"^/dev/ttys")
    (regex #"^/dev/pty")
)`),

		// 10. Read-only devices
		seatbelt.Section("Read-only devices"),
		seatbelt.Raw(`(allow file-read*
    (literal "/dev/zero")
    (literal "/dev/autofs_nowait")
    (literal "/dev/dtracehelper")
    (literal "/dev/urandom")
    (literal "/dev/random")
)`),

		// 11. File ioctl
		seatbelt.Section("File ioctl"),
		seatbelt.Raw(`(allow file-ioctl
    (literal "/dev/dtracehelper")
    (literal "/dev/tty")
    (literal "/dev/ptmx")
    (regex #"^/dev/tty")
    (regex #"^/dev/ttys")
    (regex #"^/dev/pty")
)`),

		// 12. Mach services
		seatbelt.Section("Mach services"),
		seatbelt.Raw(`(allow mach-lookup
    (global-name "com.apple.system.notification_center")
    (global-name "com.apple.system.opendirectoryd.libinfo")
    (global-name "com.apple.logd")
    (global-name "com.apple.FSEvents")
    (global-name "com.apple.SystemConfiguration.configd")
    (global-name "com.apple.SystemConfiguration.DNSConfiguration")
    (global-name "com.apple.trustd.agent")
    (global-name "com.apple.diagnosticd")
    (global-name "com.apple.analyticsd")
    (global-name "com.apple.dnssd.service")
    (global-name "com.apple.CoreServices.coreservicesd")
    (global-name "com.apple.DiskArbitration.diskarbitrationd")
    (global-name "com.apple.analyticsd.messagetracer")
    (global-name "com.apple.system.logger")
    (global-name "com.apple.coreservices.launchservicesd")
)`),

		// 13. System socket
		seatbelt.Section("System socket"),
		seatbelt.Allow("system-socket"),

		// 14. IPC shared memory
		seatbelt.Section("IPC shared memory"),
		seatbelt.Raw(`(allow ipc-posix-shm-read-data
    (ipc-posix-name "apple.shm.notification_center")
)`),
	}
}
