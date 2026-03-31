// Xcode guard for macOS Seatbelt profiles.
//
// Handles Xcode-specific filesystem paths in the home directory.
// Non-filesystem operations (Mach lookups, IOKit, signals, etc.)
// are provided by the permissive-ipc guard, which this guard is
// designed to be used alongside.

package guards

import (
	"fmt"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

type xcodeSimulatorGuard struct{}

// XcodeSimulatorGuard returns a Guard with Xcode-specific
// home directory filesystem permissions.
func XcodeSimulatorGuard() seatbelt.Guard { return &xcodeSimulatorGuard{} }

func (g *xcodeSimulatorGuard) Name() string        { return "xcode-simulator" }
func (g *xcodeSimulatorGuard) Type() string        { return "opt-in" }
func (g *xcodeSimulatorGuard) Description() string {
	return "Xcode — home directory paths for xcodebuild, simctl, and Swift PM"
}

func (g *xcodeSimulatorGuard) Rules(ctx *seatbelt.Context) seatbelt.GuardResult {
	if ctx == nil || ctx.HomeDir == "" {
		return seatbelt.GuardResult{}
	}
	home := ctx.HomeDir

	return seatbelt.GuardResult{Rules: []seatbelt.Rule{
		seatbelt.SectionAllow("Xcode home directory access"),
		seatbelt.AllowRule(fmt.Sprintf(`(allow file-read* file-write*
    (subpath "%s/Library/Preferences")
    (subpath "%s/Library/Developer")
    (subpath "%s/Library/Caches/com.apple.dt.Xcode")
    (subpath "%s/Library/org.swift.swiftpm")
    (subpath "%s/.swiftpm")
    (literal "%s/.CFUserTextEncoding")
)`, home, home, home, home, home, home)),
	}}
}
