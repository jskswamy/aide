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

func (g *nixToolchainGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	home := ctx.HomeDir

	return []seatbelt.Rule{
		// Nix store and system paths (read-only)
		seatbelt.SectionSetup("Nix store and system paths"),
		seatbelt.SetupRule(`(allow file-read*
    (subpath "/nix/store")
    (subpath "/nix/var")
    (subpath "/run/current-system")
)`),

		// Nix user paths
		seatbelt.SectionSetup("Nix user paths"),
		seatbelt.SetupRule(`(allow file-read* file-write*
    ` + seatbelt.HomeSubpath(home, ".nix-profile") + `
    ` + seatbelt.HomeSubpath(home, ".local/state/nix") + `
    ` + seatbelt.HomeSubpath(home, ".cache/nix") + `
)`),
	}
}
