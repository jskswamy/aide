// Package seatbelt provides composable macOS Seatbelt sandbox profiles.
//
// It generates .sb profile strings for use with sandbox-exec, composing
// modular rule sets (system runtime, network, filesystem, toolchains,
// agent-specific paths) into a complete profile.
//
// # Attribution
//
// The Seatbelt rules in this library — particularly the system runtime
// operations, Mach service lookups, toolchain paths, and integration
// profiles — are ported from agent-safehouse by Eugene Goldin:
// https://github.com/eugene1g/agent-safehouse
//
// Agent-safehouse provides composable Seatbelt policy profiles for AI
// coding agents and has validated profiles for 14 agents.
//
// # Usage
//
//	profile := seatbelt.New(homeDir).
//	    WithContext(func(ctx *seatbelt.Context) {
//	        ctx.ProjectRoot = projectRoot
//	        ctx.Network = "outbound"
//	    }).
//	    Use(
//	        guards.BaseGuard(),
//	        guards.SystemRuntimeGuard(),
//	        guards.NetworkGuard(),
//	        guards.FilesystemGuard(),
//	    )
//	result, err := profile.Render()
//	sbText := result.Profile
package seatbelt
