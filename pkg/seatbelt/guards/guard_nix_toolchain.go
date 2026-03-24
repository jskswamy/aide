// Nix toolchain guard for macOS Seatbelt profiles.
//
// Custom rules for Nix package manager paths.

package guards

import "github.com/jskswamy/aide/pkg/seatbelt"

type nixToolchainGuard struct{}

// NixToolchainGuard returns a Guard with Nix package manager sandbox rules.
func NixToolchainGuard() seatbelt.Guard { return &nixToolchainGuard{} }

func (g *nixToolchainGuard) Name() string        { return "nix-toolchain" }
func (g *nixToolchainGuard) Type() string        { return "always" }
func (g *nixToolchainGuard) Description() string { return "Nix store and profile access" }

func (g *nixToolchainGuard) Rules(ctx *seatbelt.Context) seatbelt.GuardResult {
	home := ctx.HomeDir

	return seatbelt.GuardResult{Rules: []seatbelt.Rule{
		// Nix store and system paths (read-only)
		seatbelt.SectionAllow("Nix store and system paths"),
		seatbelt.AllowRule(`(allow file-read*
    (subpath "/nix/store")
    (subpath "/nix/var")
    (subpath "/run/current-system")
)`),

		// Nix user paths
		seatbelt.SectionAllow("Nix user paths"),
		seatbelt.AllowRule(`(allow file-read* file-write*
    ` + seatbelt.HomeSubpath(home, ".nix-profile") + `
    ` + seatbelt.HomeSubpath(home, ".local/state/nix") + `
    ` + seatbelt.HomeSubpath(home, ".cache/nix") + `
)`),
	}}
}
