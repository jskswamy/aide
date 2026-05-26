//go:build !linux

package modules

import "github.com/jskswamy/aide/pkg/seatbelt"

// augmentLinuxPaths is a no-op on non-Linux platforms. The macOS backend
// expresses all claude path grants through GuardResult.Rules (Seatbelt DSL
// strings); Readable/Writable are unread on darwin.
func augmentLinuxPaths(_ *seatbelt.Context, _ *seatbelt.GuardResult) {}
