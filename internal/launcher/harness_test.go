package launcher

import (
	"slices"
	"testing"

	"github.com/jskswamy/aide/internal/harness"
)

// Every harness the launcher recognizes has a descriptor that owns its
// sandbox module, so no launcher table can drift from the harness table.
func TestLauncherHarnessesHaveDescriptors(t *testing.T) {
	want := []string{"aider", "amp", "claude", "codex", "copilot", "cursor-agent", "gemini", "goose", "opencode", "pi"}
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

// Aliases resolve for provisioning only; "cursor" is the Cursor editor
// binary and must not pick up the cursor-agent sandbox or discovery.
func TestAliasIsNotLaunchable(t *testing.T) {
	if IsKnownAgent("cursor") || ResolveAgentModule("cursor") != nil {
		t.Error("cursor alias treated as a launchable harness")
	}
	if _, err := YoloArgs("cursor"); err == nil {
		t.Error("YoloArgs(cursor) succeeded")
	}
}
