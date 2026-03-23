// Package guards provides composable Seatbelt profile building blocks.
package guards

import "github.com/jskswamy/aide/pkg/seatbelt"

type baseGuard struct{}

// BaseGuard returns a Guard that emits the Seatbelt version and default-deny policy.
func BaseGuard() seatbelt.Guard { return &baseGuard{} }

func (g *baseGuard) Name() string        { return "base" }
func (g *baseGuard) Type() string        { return "always" }
func (g *baseGuard) Description() string {
	return "Sandbox foundation — blocks all access unless explicitly allowed"
}

func (g *baseGuard) Rules(_ *seatbelt.Context) []seatbelt.Rule {
	return []seatbelt.Rule{
		seatbelt.Raw("(version 1)"),
		seatbelt.Raw("(deny default)"),
	}
}
