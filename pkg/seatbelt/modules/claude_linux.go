//go:build linux

package modules

import (
	"path/filepath"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

// augmentLinuxPaths populates the GuardResult with Claude's filesystem grants
// on Linux. Called from Rules() so the agent module's path declarations flow
// through the standard GuardResult pipeline alongside guard output, picking
// up audit (OriginGuard), conflict detection, and deny-wins uniformly.
func augmentLinuxPaths(ctx *seatbelt.Context, result *seatbelt.GuardResult) {
	if ctx == nil || ctx.HomeDir == "" {
		return
	}
	home := ctx.HomeDir
	result.Writable = append(result.Writable,
		filepath.Join(home, ".claude"),
		filepath.Join(home, ".claude.json"),
		filepath.Join(home, ".cache", "claude"),
		filepath.Join(home, ".local", "state", "claude"),
		filepath.Join(home, ".local", "share", "claude"),
	)
	result.Readable = append(result.Readable,
		filepath.Join(home, ".mcp.json"),
	)
}
