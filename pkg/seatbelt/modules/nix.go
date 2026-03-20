// Nix toolchain module for macOS Seatbelt profiles.
//
// Custom rules for Nix package manager paths.
package modules

import "github.com/jskswamy/aide/pkg/seatbelt"

type nixToolchainModule struct{}

// NixToolchain returns a module with Nix package manager sandbox rules.
func NixToolchain() seatbelt.Module { return &nixToolchainModule{} }

func (m *nixToolchainModule) Name() string { return "Nix Toolchain" }

func (m *nixToolchainModule) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	home := ctx.HomeDir

	return []seatbelt.Rule{
		// Nix store and system paths (read-only)
		seatbelt.Section("Nix store and system paths"),
		seatbelt.Raw(`(allow file-read*
    (subpath "/nix/store")
    (subpath "/nix/var")
    (subpath "/run/current-system")
)`),

		// Nix user paths
		seatbelt.Section("Nix user paths"),
		seatbelt.Raw(`(allow file-read* file-write*
    ` + seatbelt.HomeSubpath(home, ".nix-profile") + `
    ` + seatbelt.HomeSubpath(home, ".local/state/nix") + `
)`),
	}
}
