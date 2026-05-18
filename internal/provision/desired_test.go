package provision_test

import (
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/provision"
	"gopkg.in/yaml.v3"
)

func TestResolveDesiredMarketplaceFlat(t *testing.T) {
	y := `
plugins:
  steveyegge/beads: [beads]
  jskswamy/claude-plugins: [craft, devenv]
mcp_servers:
  rfctl: { command: rfctl }
contexts:
  default:
    agent: claude
`
	var cfg config.Config
	if err := yaml.NewDecoder(strings.NewReader(y)).Decode(&cfg); err != nil {
		t.Fatalf("decode: %v", err)
	}
	desired, err := provision.ResolveDesired(&cfg, "default")
	if err != nil {
		t.Fatal(err)
	}
	if len(desired.Marketplaces) != 2 {
		t.Errorf("marketplaces = %d, want 2", len(desired.Marketplaces))
	}
	if len(desired.Plugins) != 3 {
		t.Errorf("plugins = %d, want 3 (beads, craft, devenv): %+v", len(desired.Plugins), desired.Plugins)
	}
	if _, ok := desired.MCPServers["rfctl"]; !ok {
		t.Errorf("rfctl missing")
	}
}

// TestResolveDesiredIgnoresProjectOverride confirms that ResolveDesired
// does NOT consume cfg.ProjectOverride. Empirically establishes that
// project-scope .aide.yaml declarations cannot leak into a sync plan's
// desired set today (the launch path is the only consumer of
// ProjectOverride). If a future change wires ProjectOverride into
// sync, a trust gate must be applied first — the launch path's
// applyTrustGate is the existing model.
func TestResolveDesiredIgnoresProjectOverride(t *testing.T) {
	y := `
plugins:
  steveyegge/beads: [beads]
contexts:
  prod:
    agent: claude
`
	var cfg config.Config
	if err := yaml.NewDecoder(strings.NewReader(y)).Decode(&cfg); err != nil {
		t.Fatal(err)
	}
	// Simulate a populated, UNTRUSTED project override declaring an
	// extra MCP server. If ResolveDesired ever started merging this,
	// the assertion below would fail and we'd know to wire the trust
	// gate before that merge.
	cfg.ProjectOverride = &config.ProjectOverride{
		MCPServers: []string{"leaked-mcp"},
	}
	desired, err := provision.ResolveDesired(&cfg, "prod")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := desired.MCPServers["leaked-mcp"]; ok {
		t.Errorf("ProjectOverride.MCPServers leaked into Desired.MCPServers without trust check: %+v", desired.MCPServers)
	}
	if len(desired.MCPServers) != 0 {
		t.Errorf("expected empty MCP desired set, got %+v", desired.MCPServers)
	}
}

func TestResolveDesiredWithExclude(t *testing.T) {
	y := `
plugins:
  steveyegge/beads: [beads]
  jskswamy/claude-plugins: [craft, devenv, jot]
contexts:
  prod:
    agent: claude
    plugins:
      exclude:
        - jskswamy/claude-plugins/jot
`
	var cfg config.Config
	if err := yaml.NewDecoder(strings.NewReader(y)).Decode(&cfg); err != nil {
		t.Fatal(err)
	}
	desired, err := provision.ResolveDesired(&cfg, "prod")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := desired.Plugins["jot"]; ok {
		t.Errorf("jot should be excluded, got %+v", desired.Plugins)
	}
	if _, ok := desired.Plugins["craft"]; !ok {
		t.Errorf("craft should still be present: %+v", desired.Plugins)
	}
}
