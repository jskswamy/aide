package modules

import (
	"fmt"
	"path/filepath"
	"slices"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

// PiAgentWithInstallation adds validated installation grants without treating
// them as user-configured exceptions to credential or filesystem guards.
func PiAgentWithInstallation(root string, readable []string) seatbelt.Module {
	return &piInstallationModule{Module: PiAgent(), root: root, readable: slices.Clone(readable)}
}

type piInstallationModule struct {
	seatbelt.Module
	root     string
	readable []string
}

func (m *piInstallationModule) AgentEnvKeys(_ *seatbelt.Context) []string {
	return []string{"PI_CODING_AGENT_DIR", "XDG_DATA_HOME"}
}

// BootstrapPaths lets bubblewrap preserve lexical links without binding parents.
func (m *piInstallationModule) BootstrapPaths() []string { return slices.Clone(m.readable) }

func (m *piInstallationModule) Rules(ctx *seatbelt.Context) seatbelt.GuardResult {
	result := m.Module.Rules(ctx)
	result.Rules = append(result.Rules, seatbelt.SectionAllow("Pi managed installation"), seatbelt.AllowSubpath(m.root, "file-read*", "file-write*"))
	result.Writable = append(result.Writable, m.root)
	for _, path := range m.readable {
		result.Rules = append(result.Rules, seatbelt.AllowRule(fmt.Sprintf("(allow file-read* %s)", seatbelt.Path(path))))
	}
	result.Readable = append(result.Readable, m.readable...)
	// mkdir -p and runtime traversal stat ancestors without needing their contents.
	seen := make(map[string]bool)
	for _, path := range append([]string{m.root}, m.readable...) {
		for parent := filepath.Dir(path); ; parent = filepath.Dir(parent) {
			if !seen[parent] {
				result.Rules = append(result.Rules, seatbelt.AllowLiteral(parent, "file-read-metadata"))
				seen[parent] = true
			}
			if filepath.Dir(parent) == parent {
				break
			}
		}
	}
	return result
}

// PiAgent returns a module with Pi CLI agent sandbox rules.
// PI_CODING_AGENT_DIR is a documented env var overriding Pi's whole
// config directory (default ~/.pi/agent, per `pi --help`'s environment
// variables section). The home-relative default is the parent ".pi"
// rather than ".pi/agent" itself, in case other ".pi/*" subdirectories
// exist that the env var doesn't cover. When PI_CODING_AGENT_DIR is set
// (every Pi profile launch), only that dir is granted: not ~/.pi, so one
// profile cannot reach the default login or another profile's auth.json.
//
// Verified directly (2026-10-14, pi 1.1.0): `pi install`, `pi list` and
// `pi mcp list` against a fresh $HOME wrote only under ~/.pi/agent.
func PiAgent() seatbelt.Module {
	return NewSimpleAgent(AgentSpec{
		DisplayName:     "Pi Agent",
		SectionName:     "Pi",
		EnvKey:          "PI_CODING_AGENT_DIR",
		HomeRelDefaults: []string{".pi"},
	})
}
