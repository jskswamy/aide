//go:build !linux

package modules

import "github.com/jskswamy/aide/pkg/seatbelt"

// augmentSimpleAgentLinuxPaths is a no-op on non-Linux platforms. The macOS
// backend expresses SimpleAgent grants through GuardResult.Rules (Seatbelt
// DSL); Writable is unread on darwin.
func augmentSimpleAgentLinuxPaths(_ *seatbelt.Context, _ []string, _ *seatbelt.GuardResult) {
}
