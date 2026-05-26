//go:build !linux

package modules

import "github.com/jskswamy/aide/pkg/seatbelt"

// augmentCursorLinuxPaths is a no-op on non-Linux platforms. The macOS backend
// expresses all cursor-agent path grants through GuardResult.Rules (Seatbelt
// DSL strings); Allowed/Writable are unread on darwin.
func augmentCursorLinuxPaths(_ *seatbelt.Context, _ []string, _, _ string, _ *seatbelt.GuardResult) {
}
