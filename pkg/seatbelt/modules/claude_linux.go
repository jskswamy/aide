//go:build linux

package modules

import (
	"path/filepath"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

var _ seatbelt.LinuxAtomicWriteProvider = (*claudeAgentModule)(nil)

// augmentLinuxPaths populates the GuardResult with Linux-specific filesystem
// grants the Claude module needs from the Landlock allow-list. It is called
// from Rules() so the agent module's path declarations flow through the
// standard GuardResult pipeline alongside guard output, picking up audit
// (OriginGuard), conflict detection, and deny-wins uniformly.
//
// ~/.claude.json is intentionally absent from both Readable and Writable: it
// is declared in LinuxAtomicWritableFiles and its --bind in the overlay
// namespace already grants read+write. Listing it here too would cause
// buildAtomicWriteOverlayArgs to emit conflicting bwrap bind mounts for the
// same destination.
func augmentLinuxPaths(ctx *seatbelt.Context, result *seatbelt.GuardResult) {
	if ctx == nil || ctx.HomeDir == "" {
		return
	}
	home := ctx.HomeDir

	configDirs := resolveConfigDirs(ctx, "CLAUDE_CONFIG_DIR", []string{
		filepath.Join(home, ".claude"),
		filepath.Join(home, ".config", "claude"),
	})
	result.Writable = append(result.Writable, configDirs...)
	result.Writable = append(result.Writable,
		filepath.Join(home, ".cache", "claude"),
		filepath.Join(home, ".local", "state", "claude"),
		filepath.Join(home, ".local", "share", "claude"),
	)
	result.Readable = append(result.Readable,
		filepath.Join(home, ".mcp.json"),
	)
}

// LinuxAtomicWritableFiles declares files Claude rewrites with an open-tmp +
// rename pattern. The Linux backend overlays each file's parent with a tmpfs
// and bind-mounts only the listed files, so $HOME is not made broadly writable.
func (m *claudeAgentModule) LinuxAtomicWritableFiles(ctx *seatbelt.Context) []string {
	if ctx == nil || ctx.HomeDir == "" {
		return nil
	}
	return []string{
		filepath.Join(ctx.HomeDir, ".claude.json"),
	}
}
