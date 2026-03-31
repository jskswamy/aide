// Permissive IPC guard for macOS Seatbelt profiles.
//
// Broadly allows non-filesystem operations (Mach lookups, IOKit,
// signals, notifications, preferences, job creation, fsctl).
// Designed for complex toolchains (Xcode, Docker, Instruments, etc.)
// where enumerating individual IPC operations is impractical.
//
// The security boundary remains filesystem-based: readable/writable
// paths and network policy are still enforced by other guards.

package guards

import "github.com/jskswamy/aide/pkg/seatbelt"

type permissiveIPCGuard struct{}

// PermissiveIPCGuard returns an opt-in guard that broadly allows
// non-filesystem operations. Filesystem and network restrictions
// are still enforced by other guards.
func PermissiveIPCGuard() seatbelt.Guard { return &permissiveIPCGuard{} }

func (g *permissiveIPCGuard) Name() string { return "permissive-ipc" }
func (g *permissiveIPCGuard) Type() string { return "opt-in" }
func (g *permissiveIPCGuard) Description() string {
	return "Broad non-filesystem permissions — Mach lookups, IOKit, signals, notifications, preferences"
}

func (g *permissiveIPCGuard) Rules(_ *seatbelt.Context) seatbelt.GuardResult {
	return seatbelt.GuardResult{Rules: []seatbelt.Rule{
		// IPC: allow all Mach service lookups and IOKit device access.
		// Complex toolchains talk to many system services whose set
		// varies by OS and tool version.
		seatbelt.SectionAllow("Permissive IPC"),
		seatbelt.AllowRule("(allow mach-lookup)"),
		seatbelt.AllowRule("(allow iokit-open)"),

		// Notifications and preferences.
		seatbelt.SectionAllow("Notifications and preferences"),
		seatbelt.AllowRule("(allow distributed-notification-post)"),
		seatbelt.AllowRule("(allow user-preference-read)"),
		seatbelt.AllowRule("(allow user-preference-write)"),

		// Process control: signals and launchd job creation.
		seatbelt.SectionAllow("Process control"),
		seatbelt.AllowRule("(allow signal)"),
		seatbelt.AllowRule("(allow job-creation)"),

		// System operations.
		seatbelt.SectionAllow("System operations"),
		seatbelt.AllowRule("(allow system-fsctl)"),
	}}
}
