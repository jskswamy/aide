package launcher

import (
	"slices"
	"testing"

	"github.com/jskswamy/aide/internal/harness"
)

// Every harness the launcher recognizes has a descriptor that owns its
// sandbox module, so no launcher table can drift from the harness table.
func TestLauncherHarnessesHaveDescriptors(t *testing.T) {
	want := []string{"agy", "aider", "amp", "claude", "codex", "copilot", "cursor-agent", "gemini", "goose", "opencode", "pi"}
	if !slices.Equal(KnownAgents, want) {
		t.Errorf("KnownAgents = %v, want %v", KnownAgents, want)
	}
	for _, name := range KnownAgents {
		d, ok := harness.Lookup(name)
		if !ok {
			t.Errorf("%q has no harness descriptor", name)
			continue
		}
		if d.Name != name || !d.Launchable() {
			t.Errorf("%q: descriptor %q launchable=%v", name, d.Name, d.Launchable())
		}
	}
}

// Hermes has a provision driver but no sandbox module, so it is not
// discovered on PATH.
func TestHermesIsNotLaunchable(t *testing.T) {
	if IsKnownAgent("hermes") || ResolveAgentModule("hermes") != nil {
		t.Error("hermes treated as a launchable harness")
	}
}

func TestAliasIsNotLaunchable(t *testing.T) {
	for _, alias := range []string{"cursor", "antigravity"} {
		if IsKnownAgent(alias) || ResolveAgentModule(alias) != nil {
			t.Errorf("%s alias treated as a launchable harness", alias)
		}
		if _, err := YoloArgs(alias); err == nil {
			t.Errorf("YoloArgs(%s) succeeded", alias)
		}
	}
}
