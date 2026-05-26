//go:build linux

package modules

import "github.com/jskswamy/aide/pkg/seatbelt"

// augmentCursorLinuxPaths populates GuardResult with Landlock path grants for
// the cursor-agent CLI on Linux. The macOS backend expresses all grants through
// GuardResult.Rules (Seatbelt DSL); Allowed/Writable are unread on darwin, so
// this file only compiles on Linux.
//
// Paths granted:
//   - configDirs (e.g. ~/.config/cursor, ~/.cursor) → Writable: cursor-agent
//     reads and writes auth.json, settings, and cached state here.
//   - logsDir (e.g. ~/.local/share/cursor-agent/logs) → Writable: log output.
//   - activeVerDir (e.g. ~/.local/share/cursor-agent/versions/<v>) → Allowed:
//     read-only; the binary itself lives here and may read its own files.
func augmentCursorLinuxPaths(ctx *seatbelt.Context, configDirs []string, logsDir, activeVerDir string, result *seatbelt.GuardResult) {
	if ctx == nil || ctx.HomeDir == "" {
		return
	}
	result.Writable = append(result.Writable, configDirs...)
	if logsDir != "" {
		result.Writable = append(result.Writable, logsDir)
	}
	if activeVerDir != "" {
		result.Allowed = append(result.Allowed, activeVerDir)
	}
}
