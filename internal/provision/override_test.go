package provision_test

import (
	"testing"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/provision"
)

func TestApplyOverrideNilInheritsAll(t *testing.T) {
	top := map[string]config.PluginEntry{
		"a/b": {},
	}
	got := provision.ApplyOverride(top, nil)
	if _, ok := got["a/b"]; !ok {
		t.Errorf("nil override should inherit all")
	}
}

func TestApplyOverrideExcludeRemoves(t *testing.T) {
	top := map[string]config.PluginEntry{
		"a/b": {},
		"c/d": {},
	}
	override := &config.ContextOverride[config.PluginEntry]{
		Exclude: []string{"a/b"},
	}
	got := provision.ApplyOverride(top, override)
	if _, ok := got["a/b"]; ok {
		t.Errorf("a/b should be excluded")
	}
	if _, ok := got["c/d"]; !ok {
		t.Errorf("c/d should remain")
	}
}

func TestApplyOverrideOnlyReplaces(t *testing.T) {
	top := map[string]config.PluginEntry{
		"a/b": {},
		"c/d": {},
	}
	override := &config.ContextOverride[config.PluginEntry]{
		Only: []string{"a/b"},
	}
	got := provision.ApplyOverride(top, override)
	if len(got) != 1 {
		t.Errorf("only mode should yield exactly 1 entry, got %d", len(got))
	}
}

func TestApplyOverrideOnlySubpathKeepsOneSubPlugin(t *testing.T) {
	// only: [repo/plugin] should yield an entry with just that one
	// plugin, not the whole marketplace's plugin list.
	top := map[string]config.PluginEntry{
		"jskswamy/claude-plugins": config.PluginEntryMarketplace([]string{"craft", "devenv", "jot", "codebase"}),
		"steveyegge/beads":        config.PluginEntryMarketplace([]string{"beads"}),
	}
	override := &config.ContextOverride[config.PluginEntry]{
		Only: []string{"jskswamy/claude-plugins/craft"},
	}
	got := provision.ApplyOverride(top, override)
	if len(got) != 1 {
		t.Fatalf("expected exactly 1 entry, got %d: %+v", len(got), got)
	}
	entry, ok := got["jskswamy/claude-plugins"]
	if !ok {
		t.Fatal("entry under jskswamy/claude-plugins missing")
	}
	if len(entry.Plugins) != 1 || entry.Plugins[0] != "craft" {
		t.Errorf("entry.Plugins = %v, want [craft]", entry.Plugins)
	}
}

func TestApplyOverrideOnlyMultipleSubpathsSameRepoAccumulate(t *testing.T) {
	// only: [repo/p1, repo/p2] should yield one entry with both plugins.
	top := map[string]config.PluginEntry{
		"jskswamy/claude-plugins": config.PluginEntryMarketplace([]string{"craft", "devenv", "jot"}),
	}
	override := &config.ContextOverride[config.PluginEntry]{
		Only: []string{"jskswamy/claude-plugins/craft", "jskswamy/claude-plugins/jot"},
	}
	got := provision.ApplyOverride(top, override)
	entry := got["jskswamy/claude-plugins"]
	if len(entry.Plugins) != 2 {
		t.Fatalf("expected 2 plugins, got %v", entry.Plugins)
	}
	have := map[string]bool{}
	for _, p := range entry.Plugins {
		have[p] = true
	}
	if !have["craft"] || !have["jot"] {
		t.Errorf("missing expected plugins: %v", entry.Plugins)
	}
	if have["devenv"] {
		t.Errorf("devenv should NOT be present: %v", entry.Plugins)
	}
}

func TestApplyOverrideOnlyMixedWholeAndSubpath(t *testing.T) {
	// only: [wholeRepo, otherRepo/specific] mixes whole-entry and
	// subpath modes. Each should be respected independently.
	top := map[string]config.PluginEntry{
		"steveyegge/beads":        config.PluginEntryMarketplace([]string{"beads"}),
		"jskswamy/claude-plugins": config.PluginEntryMarketplace([]string{"craft", "devenv", "jot"}),
	}
	override := &config.ContextOverride[config.PluginEntry]{
		Only: []string{"steveyegge/beads", "jskswamy/claude-plugins/jot"},
	}
	got := provision.ApplyOverride(top, override)
	if len(got) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(got))
	}
	if entry := got["steveyegge/beads"]; len(entry.Plugins) != 1 || entry.Plugins[0] != "beads" {
		t.Errorf("steveyegge/beads = %v, want [beads]", entry.Plugins)
	}
	if entry := got["jskswamy/claude-plugins"]; len(entry.Plugins) != 1 || entry.Plugins[0] != "jot" {
		t.Errorf("jskswamy/claude-plugins = %v, want [jot]", entry.Plugins)
	}
}

func TestApplyOverrideExtraAdds(t *testing.T) {
	top := map[string]config.PluginEntry{"a/b": {}}
	override := &config.ContextOverride[config.PluginEntry]{
		Extra: map[string]config.PluginEntry{"e/f": {}},
	}
	got := provision.ApplyOverride(top, override)
	if _, ok := got["e/f"]; !ok {
		t.Errorf("extra entry should be added")
	}
}
