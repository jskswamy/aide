package modules

import (
	"path/filepath"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

// AgentSpec is the data-only shape of an "I have one config dir under
// $HOME plus an optional env-var override" agent module. Five of the
// seven bundled agent modules (aider, amp, codex, gemini, goose) match
// this shape; copilot and cursor-agent diverge and keep their own Rules method.
type AgentSpec struct {
	// DisplayName is what Module.Name returns (e.g. "Aider Agent").
	DisplayName string
	// SectionName is used as the profile section header (e.g. "Aider").
	SectionName string
	// EnvKey is the env var that, when set non-empty, overrides the
	// default dir list with that single path. Empty means the agent has
	// no override mechanism.
	EnvKey string
	// HomeRelDefaults are HomeDir-relative paths to consider when no
	// override is set; each is filepath.Joined onto ctx.HomeDir at
	// rule-generation time.
	HomeRelDefaults []string
}

type simpleAgentModule struct {
	spec AgentSpec
}

// NewSimpleAgent builds a Module from an AgentSpec.
func NewSimpleAgent(spec AgentSpec) seatbelt.Module {
	return &simpleAgentModule{spec: spec}
}

func (m *simpleAgentModule) Name() string { return m.spec.DisplayName }

func (m *simpleAgentModule) Rules(ctx *seatbelt.Context) seatbelt.GuardResult {
	defaults := make([]string, 0, len(m.spec.HomeRelDefaults))
	for _, rel := range m.spec.HomeRelDefaults {
		defaults = append(defaults, filepath.Join(ctx.HomeDir, rel))
	}
	dirs := resolveConfigDirs(ctx, m.spec.EnvKey, defaults)
	return seatbelt.GuardResult{Rules: configDirRules(m.spec.SectionName, ctx.HomeDir, dirs)}
}
