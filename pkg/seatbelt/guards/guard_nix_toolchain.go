// Nix toolchain guard for macOS Seatbelt profiles.
//
// Custom rules for Nix package manager paths.

package guards

import (
	"fmt"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

type nixToolchainGuard struct{}

// NixToolchainGuard returns a Guard with Nix package manager sandbox rules.
func NixToolchainGuard() seatbelt.Guard { return &nixToolchainGuard{} }

func (g *nixToolchainGuard) Name() string        { return "nix-toolchain" }
func (g *nixToolchainGuard) Type() string        { return "always" }
func (g *nixToolchainGuard) Description() string { return "Nix store and profile access" }

func (g *nixToolchainGuard) Rules(ctx *seatbelt.Context) seatbelt.GuardResult {
	if ctx == nil || ctx.HomeDir == "" {
		return seatbelt.GuardResult{}
	}
	if !dirExists("/nix/store") {
		return seatbelt.GuardResult{
			Skipped: []string{"/nix/store not found — nix not installed"},
		}
	}

	home := ctx.HomeDir

	return seatbelt.GuardResult{Rules: []seatbelt.Rule{
		// Nix daemon socket
		seatbelt.SectionAllow("Nix daemon socket"),
		seatbelt.AllowRule(`(allow network-outbound
    (remote unix-socket (path-literal "/nix/var/nix/daemon-socket/socket"))
)`),

		// Nix user paths (read+write, self-contained)
		seatbelt.SectionAllow("Nix user paths"),
		seatbelt.AllowRule(fmt.Sprintf(`(allow file-read* file-write*
    %s
    %s
    %s
)`, seatbelt.HomeSubpath(home, ".nix-profile"),
			seatbelt.HomeSubpath(home, ".local/state/nix"),
			seatbelt.HomeSubpath(home, ".cache/nix"))),

		// Nix channel definitions and user config (read-only)
		seatbelt.SectionAllow("Nix channel definitions and user config"),
		seatbelt.AllowRule(fmt.Sprintf(`(allow file-read*
    %s
    %s
)`, seatbelt.HomeSubpath(home, ".nix-defexpr"),
			seatbelt.HomeSubpath(home, ".config/nix"))),
	}}
}
