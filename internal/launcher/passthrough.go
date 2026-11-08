package launcher

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/harness"
)

// KnownAgents is the list of agent binaries aide can detect on PATH:
// every launchable harness, in name order.
var KnownAgents = knownAgents()

func knownAgents() []string {
	var names []string
	for _, d := range harness.All() {
		if d.Launchable() {
			names = append(names, d.Name)
		}
	}
	return names
}

// LookPathFunc abstracts exec.LookPath for testability.
type LookPathFunc func(file string) (string, error)

// PassthroughResult describes what the agent PATH scanner found.
type PassthroughResult struct {
	// Found agents with their resolved paths.
	Found map[string]string
}

// IsKnownAgent returns true if the agent name is in KnownAgents.
func IsKnownAgent(name string) bool {
	for _, a := range KnownAgents {
		if a == name {
			return true
		}
	}
	return false
}

// ValidateAgentOverride accepts an --agent value that is a known agent,
// an agent defined in config, or a name resolvable on PATH.
func ValidateAgentOverride(cfg *config.Config, agent string, lookPath LookPathFunc) error {
	if _, inAgentsMap := cfg.Agents[agent]; inAgentsMap || IsKnownAgent(agent) {
		return nil
	}
	if _, err := lookPath(agent); err != nil {
		return fmt.Errorf(
			"unknown agent %q (not in known agents, config, or PATH).\n\n"+
				"Register it first: aide agents add %s --binary /path/to/binary\n"+
				"Known agents: %s",
			agent, agent, strings.Join(KnownAgents, ", "),
		)
	}
	return nil
}

// ScanAgents scans PATH for known agent binaries.
func ScanAgents(lookPath LookPathFunc) *PassthroughResult {
	found := make(map[string]string)
	for _, d := range harness.All() {
		if !d.Launchable() {
			continue
		}
		if path, err := lookPath(d.Binary); err == nil {
			found[d.Name] = path
		}
	}
	return &PassthroughResult{Found: found}
}

// lookPath returns the LookPathFunc to use (real or injected for testing).
func (l *Launcher) lookPath() LookPathFunc {
	if l.LookPath != nil {
		return l.LookPath
	}
	return exec.LookPath
}
