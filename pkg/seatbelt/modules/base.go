// Package modules provides composable Seatbelt profile building blocks.
package modules

import "github.com/jskswamy/aide/pkg/seatbelt"

type baseModule struct{}

// Base returns a module that emits the Seatbelt version and default-deny policy.
func Base() seatbelt.Module { return &baseModule{} }

func (m *baseModule) Name() string { return "Base" }

func (m *baseModule) Rules(_ *seatbelt.Context) []seatbelt.Rule {
	return []seatbelt.Rule{
		seatbelt.Raw("(version 1)"),
		seatbelt.Raw("(deny default)"),
	}
}
