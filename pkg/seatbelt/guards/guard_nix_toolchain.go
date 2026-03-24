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
	if !dirExists("/nix/store") {
		return seatbelt.GuardResult{
			Skipped: []string{"/nix/store not found — nix not installed"},
		}
	}

	home := ctx.HomeDir

	rules := []seatbelt.Rule{
		// Parent directory metadata for symlink traversal (/run firmlink)
		seatbelt.SectionAllow("Nix parent directory metadata"),
		seatbelt.AllowRule(`(allow file-read-metadata
    (literal "/run")
)`),
	}

	// Nix store with parent metadata (uses helper to ensure /nix lstat works)
	rules = append(rules, seatbelt.SectionAllow("Nix store and system paths"))
	rules = append(rules, seatbelt.SubpathWithParentMetadata("/nix/store")...)
	rules = append(rules, seatbelt.AllowRule(`(allow file-read*
    (subpath "/nix/var")
    (subpath "/run/current-system")
    (subpath "/private/var/run/current-system")
)`))

	rules = append(rules,
		// Nix daemon socket
		seatbelt.SectionAllow("Nix daemon socket"),
		seatbelt.AllowRule(`(allow network-outbound
    (remote unix-socket (path-literal "/nix/var/nix/daemon-socket/socket"))
)`),

		// Nix user paths (read-write)
		seatbelt.SectionAllow("Nix user paths"),
		seatbelt.AllowRule(`(allow file-read* file-write*
    `+seatbelt.HomeSubpath(home, ".nix-profile")+`
    `+seatbelt.HomeSubpath(home, ".local/state/nix")+`
    `+seatbelt.HomeSubpath(home, ".cache/nix")+`
)`),

		// Nix channel definitions and user config (read-only)
		seatbelt.SectionAllow("Nix channel definitions and user config"),
		seatbelt.AllowRule(`(allow file-read*
    `+seatbelt.HomeSubpath(home, ".nix-defexpr")+`
    `+seatbelt.HomeSubpath(home, ".config/nix")+`
)`),
	)

	return seatbelt.GuardResult{Rules: rules}
}
