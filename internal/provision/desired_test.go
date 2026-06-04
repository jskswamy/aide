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

func TestResolveDesiredHooksBasic(t *testing.T) {
	y := `
hooks:
  pre_tool:
    - matcher: shell
      command: rtk hook {agent}
  session_start:
    - command: bd prime
contexts:
  work:
    agent: claude
`
	var cfg config.Config
	if err := yaml.NewDecoder(strings.NewReader(y)).Decode(&cfg); err != nil {
		t.Fatalf("decode: %v", err)
	}
	desired, err := provision.ResolveDesired(&cfg, "work")
	if err != nil {
		t.Fatal(err)
	}
	if len(desired.Hooks) != 2 {
		t.Fatalf("Hooks = %d, want 2: %+v", len(desired.Hooks), desired.Hooks)
	}
	var preTool provision.Hook
	for _, h := range desired.Hooks {
		if h.Event == "pre_tool" {
			preTool = h
		}
	}
	if preTool.Command != "rtk hook claude" {
		t.Errorf("command after {agent} substitution = %q, want %q", preTool.Command, "rtk hook claude")
	}
	if preTool.Matcher != "shell" {
		t.Errorf("matcher = %q, want %q", preTool.Matcher, "shell")
	}
}

func TestResolveDesiredHooksContextOverride(t *testing.T) {
	y := `
hooks:
  pre_tool:
    - matcher: shell
      command: rtk hook {agent}
  session_start:
    - command: bd prime
contexts:
  personal:
    agent: gemini
    hooks:
      exclude: [session_start]
      extra:
        pre_tool:
          - command: personal-hook
`
	var cfg config.Config
	if err := yaml.NewDecoder(strings.NewReader(y)).Decode(&cfg); err != nil {
		t.Fatalf("decode: %v", err)
	}
	desired, err := provision.ResolveDesired(&cfg, "personal")
	if err != nil {
		t.Fatal(err)
	}
	// session_start excluded, pre_tool has 2 entries (inherited + extra)
	var sessionStart, preTool []provision.Hook
	for _, h := range desired.Hooks {
		switch h.Event {
		case "session_start":
			sessionStart = append(sessionStart, h)
		case "pre_tool":
			preTool = append(preTool, h)
		}
	}
	if len(sessionStart) != 0 {
		t.Errorf("session_start should be excluded, got %v", sessionStart)
	}
	if len(preTool) != 2 {
		t.Errorf("pre_tool should have 2 entries (inherited + extra), got %d", len(preTool))
	}
	found := false
	for _, h := range preTool {
		if h.Command == "rtk hook gemini" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected rtk hook gemini in pre_tool hooks: %+v", preTool)
	}
}

func TestResolveDesiredHooksExcludeByName(t *testing.T) {
	y := `
hooks:
  pre_tool:
    - command: global-guard
      name: guard
      matcher: shell
    - command: audit-log
      name: audit
contexts:
  personal:
    agent: claude
    hooks:
      exclude_hooks:
        pre_tool: [guard]
`
	var cfg config.Config
	if err := yaml.NewDecoder(strings.NewReader(y)).Decode(&cfg); err != nil {
		t.Fatalf("decode: %v", err)
	}
	desired, err := provision.ResolveDesired(&cfg, "personal")
	if err != nil {
		t.Fatal(err)
	}
	var preTool []provision.Hook
	for _, h := range desired.Hooks {
		if h.Event == "pre_tool" {
			preTool = append(preTool, h)
		}
	}
	if len(preTool) != 1 {
		t.Fatalf("want 1 pre_tool hook (audit survives), got %d: %+v", len(preTool), preTool)
	}
	if preTool[0].Command != "audit-log" {
		t.Errorf("surviving hook = %q, want %q", preTool[0].Command, "audit-log")
	}
}

func TestResolveDesiredHooksExcludeByNameUnnamedSurvives(t *testing.T) {
	y := `
hooks:
  pre_tool:
    - command: unnamed-hook
    - command: named-hook
      name: named
contexts:
  personal:
    agent: claude
    hooks:
      exclude_hooks:
        pre_tool: [named]
`
	var cfg config.Config
	if err := yaml.NewDecoder(strings.NewReader(y)).Decode(&cfg); err != nil {
		t.Fatalf("decode: %v", err)
	}
	desired, err := provision.ResolveDesired(&cfg, "personal")
	if err != nil {
		t.Fatal(err)
	}
	var preTool []provision.Hook
	for _, h := range desired.Hooks {
		if h.Event == "pre_tool" {
			preTool = append(preTool, h)
		}
	}
	if len(preTool) != 1 {
		t.Fatalf("want 1 pre_tool hook (unnamed survives), got %d: %+v", len(preTool), preTool)
	}
	if preTool[0].Command != "unnamed-hook" {
		t.Errorf("surviving hook = %q, want %q", preTool[0].Command, "unnamed-hook")
	}
}

func TestResolveDesiredHooksExcludeAllNamedEventDropped(t *testing.T) {
	y := `
hooks:
  pre_tool:
    - command: only-hook
      name: only
contexts:
  personal:
    agent: claude
    hooks:
      exclude_hooks:
        pre_tool: [only]
`
	var cfg config.Config
	if err := yaml.NewDecoder(strings.NewReader(y)).Decode(&cfg); err != nil {
		t.Fatalf("decode: %v", err)
	}
	desired, err := provision.ResolveDesired(&cfg, "personal")
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range desired.Hooks {
		if h.Event == "pre_tool" {
			t.Errorf("expected no pre_tool hooks, got %+v", h)
		}
	}
}
