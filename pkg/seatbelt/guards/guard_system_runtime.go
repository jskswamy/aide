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

func (g *systemRuntimeGuard) Rules(ctx *seatbelt.Context) seatbelt.GuardResult {
	if ctx == nil {
		return seatbelt.GuardResult{}
	}
	rules := []seatbelt.Rule{
		// 1. Broad system reads — all top-level system directories
		seatbelt.SectionAllow("Broad system reads"),
		seatbelt.AllowRule(`(allow file-read*
    (subpath "/usr")
    (subpath "/bin")
    (subpath "/sbin")
    (subpath "/opt")
    (subpath "/System")
    (subpath "/Library")
    (subpath "/nix")
    (subpath "/etc")
    (subpath "/private")
    (subpath "/Applications")
    (subpath "/run")
    (subpath "/dev")
    (subpath "/tmp")
    (subpath "/var")
)`),

		// 2. Root-level traversal
		seatbelt.SectionAllow("Root-level traversal"),
		seatbelt.AllowRule(`(allow file-read-metadata
    (literal "/")
    (literal "/Users")
)`),
		seatbelt.AllowRule(`(allow file-read-data
    (literal "/")
)`),

		// 3. Process rules
		seatbelt.SectionAllow("Process rules"),

		seatbelt.AllowRule("(allow process-exec)"),
		seatbelt.AllowRule("(allow sysctl-read)"),
		seatbelt.AllowRule("(allow process-info* (target same-sandbox))"),
		seatbelt.AllowRule("(allow signal (target same-sandbox))"),
		seatbelt.AllowRule("(allow mach-priv-task-port (target same-sandbox))"),
		seatbelt.AllowRule("(allow pseudo-tty)"),

		// 7. Temp dirs
		seatbelt.SectionAllow("Temp dirs"),
		seatbelt.AllowRule(`(allow file-read* file-write*
    (subpath "/tmp")
    (subpath "/private/tmp")
    (subpath "/var/folders")
    (subpath "/private/var/folders")
)`),

		// 8. Launchd listener deny
		seatbelt.SectionAllow("Launchd listener deny"),
		seatbelt.AllowRule(`(deny file-read* file-write*
    (regex #"^/private/tmp/com\.apple\.launchd\.[^/]+/Listeners$")
    (regex #"^/tmp/com\.apple\.launchd\.[^/]+/Listeners$")
)`),

		// 9. Device nodes (read-write)
		seatbelt.SectionAllow("Device nodes"),
		seatbelt.AllowRule(`(allow file-read* file-write*
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
		seatbelt.SectionAllow("Read-only devices"),
		seatbelt.AllowRule(`(allow file-read*
    (literal "/dev/zero")
    (literal "/dev/autofs_nowait")
    (literal "/dev/dtracehelper")
    (literal "/dev/urandom")
    (literal "/dev/random")
)`),

		// 11. File ioctl
		seatbelt.SectionAllow("File ioctl"),
		seatbelt.AllowRule(`(allow file-ioctl
    (literal "/dev/dtracehelper")
    (literal "/dev/tty")
    (literal "/dev/ptmx")
    (regex #"^/dev/tty")
    (regex #"^/dev/ttys")
    (regex #"^/dev/pty")
)`),

		// 12. Mach services
		seatbelt.SectionAllow("Mach services"),
		seatbelt.AllowRule(`(allow mach-lookup
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
		seatbelt.SectionAllow("System socket"),
		seatbelt.AllowRule("(allow system-socket)"),

		// 14. IPC shared memory
		seatbelt.SectionAllow("IPC shared memory"),
		seatbelt.AllowRule(`(allow ipc-posix-shm-read-data
    (ipc-posix-name "apple.shm.notification_center")
)`),
	}

	// Conditional process-fork based on AllowSubprocess
	if ctx.AllowSubprocess {
		rules = append(rules, seatbelt.AllowRule("(allow process-fork)"))
	} else {
		rules = append(rules, seatbelt.DenyRule("(deny process-fork)"))
	}

	return seatbelt.GuardResult{Rules: rules}
}
