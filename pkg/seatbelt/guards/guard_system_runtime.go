// System runtime guard for macOS Seatbelt profiles.
//
// Rules ported from agent-safehouse by Eugene Goldin:
// https://github.com/eugene1g/agent-safehouse
// Source: profiles/10-system-runtime.sb

package guards

import "github.com/jskswamy/aide/pkg/seatbelt"

type systemRuntimeGuard struct{}

// SystemRuntimeGuard returns a Guard with macOS system runtime rules.
func SystemRuntimeGuard() seatbelt.Guard { return &systemRuntimeGuard{} }

func (g *systemRuntimeGuard) Name() string        { return "system-runtime" }
func (g *systemRuntimeGuard) Type() string        { return "always" }
func (g *systemRuntimeGuard) Description() string {
	return "System binaries, devices, and OS services for agent operation"
}

func (g *systemRuntimeGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	home := ctx.HomeDir

	return []seatbelt.Rule{
		// 1. System binary paths
		seatbelt.SectionSetup("System binary paths"),
		seatbelt.SetupRule(`(allow file-read*
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
		seatbelt.SectionSetup("Root filesystem traversal"),
		seatbelt.SetupRule(`(allow file-read-data
    (literal "/")
)`),

		// 3. Metadata traversal
		seatbelt.SectionSetup("Metadata traversal"),
		// Git requires stat() on parent directories up to / for its
		// safe.directory ownership check. /Users is needed on macOS
		// so git can walk /Users → /Users/<user> → ... → repo root.
		seatbelt.SetupRule(`(allow file-read-metadata
    (literal "/")
    (literal "/Users")
    (subpath "/System")
    (subpath "/private/var/run")
)`),

		// 3. Private/etc paths
		seatbelt.SectionSetup("Private/etc paths"),
		seatbelt.SetupRule(`(allow file-read*
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
		seatbelt.SectionSetup("Home metadata traversal"),
		seatbelt.SetupRule(`(allow file-read-metadata
    (literal "/home")
    (literal "/private/etc")
    (subpath "/dev")
    ` + seatbelt.HomeLiteral(home, ".config") + `
    ` + seatbelt.HomeLiteral(home, ".cache") + `
    ` + seatbelt.HomeLiteral(home, ".local") + `
    ` + seatbelt.HomeLiteral(home, ".local/share") + `
)`),

		// 5. User preferences
		seatbelt.SectionSetup("User preferences"),
		seatbelt.SetupRule(`(allow file-read*
    ` + seatbelt.HomePrefix(home, "Library/Preferences/.GlobalPreferences") + `
    ` + seatbelt.HomePrefix(home, "Library/Preferences/com.apple.GlobalPreferences") + `
    ` + seatbelt.HomeSubpath(home, "Library/Preferences/ByHost") + `
    ` + seatbelt.HomeLiteral(home, ".CFUserTextEncoding") + `
    ` + seatbelt.HomeLiteral(home, ".config") + `
    ` + seatbelt.HomeLiteral(home, ".cache") + `
    ` + seatbelt.HomeLiteral(home, ".local/bin") + `
)`),

		// 6. Process rules
		seatbelt.SectionSetup("Process rules"),
		seatbelt.SetupRule("(allow process-exec)"),
		seatbelt.SetupRule("(allow process-fork)"),
		seatbelt.SetupRule("(allow sysctl-read)"),
		seatbelt.SetupRule("(allow process-info* (target same-sandbox))"),
		seatbelt.SetupRule("(allow signal (target same-sandbox))"),
		seatbelt.SetupRule("(allow mach-priv-task-port (target same-sandbox))"),
		seatbelt.SetupRule("(allow pseudo-tty)"),

		// 7. Temp dirs
		seatbelt.SectionSetup("Temp dirs"),
		seatbelt.SetupRule(`(allow file-read* file-write*
    (subpath "/tmp")
    (subpath "/private/tmp")
    (subpath "/var/folders")
    (subpath "/private/var/folders")
)`),

		// 8. Launchd listener deny
		seatbelt.SectionSetup("Launchd listener deny"),
		seatbelt.SetupRule(`(deny file-read* file-write*
    (regex #"^/private/tmp/com\.apple\.launchd\.[^/]+/Listeners$")
    (regex #"^/tmp/com\.apple\.launchd\.[^/]+/Listeners$")
)`),

		// 9. Device nodes (read-write)
		seatbelt.SectionSetup("Device nodes"),
		seatbelt.SetupRule(`(allow file-read* file-write*
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
		seatbelt.SectionSetup("Read-only devices"),
		seatbelt.SetupRule(`(allow file-read*
    (literal "/dev/zero")
    (literal "/dev/autofs_nowait")
    (literal "/dev/dtracehelper")
    (literal "/dev/urandom")
    (literal "/dev/random")
)`),

		// 11. File ioctl
		seatbelt.SectionSetup("File ioctl"),
		seatbelt.SetupRule(`(allow file-ioctl
    (literal "/dev/dtracehelper")
    (literal "/dev/tty")
    (literal "/dev/ptmx")
    (regex #"^/dev/tty")
    (regex #"^/dev/ttys")
    (regex #"^/dev/pty")
)`),

		// 12. Mach services
		seatbelt.SectionSetup("Mach services"),
		seatbelt.SetupRule(`(allow mach-lookup
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
		seatbelt.SectionSetup("System socket"),
		seatbelt.SetupRule("(allow system-socket)"),

		// 14. IPC shared memory
		seatbelt.SectionSetup("IPC shared memory"),
		seatbelt.SetupRule(`(allow ipc-posix-shm-read-data
    (ipc-posix-name "apple.shm.notification_center")
)`),
	}
}
