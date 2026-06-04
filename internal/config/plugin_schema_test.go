package config_test

import (
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/config"
	"gopkg.in/yaml.v3"
)

func TestPluginsPolymorphicShapes(t *testing.T) {
	y := `
plugins:
  steveyegge/beads: [beads]
  jskswamy/claude-plugins:
    - craft
    - devenv
  gemini-cli-tool: "github:google/gemini-cli-tool"
  obra/superpowers-marketplace: ~
`
	var cfg config.Config
	if err := yaml.NewDecoder(strings.NewReader(y)).Decode(&cfg); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(cfg.Plugins) != 4 {
		t.Fatalf("expected 4 entries, got %d", len(cfg.Plugins))
	}

	mk := cfg.Plugins["steveyegge/beads"]
	if mk.Shape() != config.PluginShapeMarketplace {
		t.Errorf("steveyegge/beads shape = %v, want marketplace", mk.Shape())
	}
	if len(mk.Plugins) != 1 || mk.Plugins[0] != "beads" {
		t.Errorf("steveyegge/beads plugins = %v", mk.Plugins)
	}

	multi := cfg.Plugins["jskswamy/claude-plugins"]
	if len(multi.Plugins) != 2 {
		t.Errorf("jskswamy/claude-plugins plugins = %v", multi.Plugins)
	}

	ud := cfg.Plugins["gemini-cli-tool"]
	if ud.Shape() != config.PluginShapeURLDirect {
		t.Errorf("gemini-cli-tool shape = %v, want url-direct", ud.Shape())
	}
	if ud.Source != "github:google/gemini-cli-tool" {
		t.Errorf("gemini-cli-tool source = %q", ud.Source)
	}

	dec, ok := cfg.Plugins["obra/superpowers-marketplace"]
	if !ok {
		t.Fatalf("missing obra/superpowers-marketplace entry; keys = %v", keys(cfg.Plugins))
	}
	if dec.Shape() != config.PluginShapeDeclareOnly {
		t.Errorf("declare-only shape = %v", dec.Shape())
	}
}

func keys(m map[string]config.PluginEntry) []string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}

func TestContextPluginsOverrideInheritsDefault(t *testing.T) {
	y := `
plugins:
  steveyegge/beads: [beads]
contexts:
  default:
    agent: claude
`
	var cfg config.Config
	if err := yaml.NewDecoder(strings.NewReader(y)).Decode(&cfg); err != nil {
		t.Fatal(err)
	}
	ctx := cfg.Contexts["default"]
	if ctx.Plugins != nil {
		t.Errorf("absent block should yield nil ContextOverride, got %+v", ctx.Plugins)
	}
}

func TestContextPluginsOverrideExcludeExtra(t *testing.T) {
	y := `
plugins:
  steveyegge/beads: [beads]
contexts:
  prod:
    agent: claude
    plugins:
      exclude:
        - obra/superpowers-marketplace/double-shot-latte
      extra:
        my-org/internal: [private-tool]
`
	var cfg config.Config
	if err := yaml.NewDecoder(strings.NewReader(y)).Decode(&cfg); err != nil {
		t.Fatal(err)
	}
	override := cfg.Contexts["prod"].Plugins
	if override == nil {
		t.Fatal("expected non-nil ContextOverride")
		return
	}
	if len(override.Exclude) != 1 || override.Exclude[0] != "obra/superpowers-marketplace/double-shot-latte" {
		t.Errorf("exclude = %v", override.Exclude)
	}
	if _, ok := override.Extra["my-org/internal"]; !ok {
		t.Errorf("extra missing my-org/internal: %+v", override.Extra)
	}
}

func TestMCPServersTopLevelMap(t *testing.T) {
	y := `
mcp_servers:
  postgres:
    command: postgres-mcp
    args: ["--port", "5432"]
  slack:
    url: https://mcp.slack.app/sse
`
	var cfg config.Config
	if err := yaml.NewDecoder(strings.NewReader(y)).Decode(&cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.MCPServers) != 2 {
		t.Fatalf("expected 2 mcp_servers, got %d", len(cfg.MCPServers))
	}
	if cfg.MCPServers["postgres"].Command != "postgres-mcp" {
		t.Errorf("postgres command = %q", cfg.MCPServers["postgres"].Command)
	}
}

func TestPluginsMarketplaceKeyMustLookLikeRepo(t *testing.T) {
	y := `
plugins:
  just-a-name: [foo]
`
	var cfg config.Config
	if err := yaml.NewDecoder(strings.NewReader(y)).Decode(&cfg); err != nil {
		t.Fatal(err)
	}
	err := cfg.ValidatePlugins()
	if err == nil || !strings.Contains(err.Error(), "just-a-name") {
		t.Errorf("expected error naming the bad key, got %v", err)
	}
}

func TestPluginsURLDirectKeyMustNotHaveSlash(t *testing.T) {
	y := `
plugins:
  steveyegge/beads: "github:foo/bar"
`
	var cfg config.Config
	if err := yaml.NewDecoder(strings.NewReader(y)).Decode(&cfg); err != nil {
		// May error at decode time already due to Task 2 wiring; either is fine.
		return
	}
	err := cfg.ValidatePlugins()
	if err == nil {
		t.Error("expected error: slash-key with string value")
	}
}

func TestContextPluginsExcludePathMustResolve(t *testing.T) {
	y := `
plugins:
  steveyegge/beads: [beads]
contexts:
  ci:
    agent: claude
    plugins:
      exclude: [does-not-exist]
`
	var cfg config.Config
	_ = yaml.NewDecoder(strings.NewReader(y)).Decode(&cfg)
	err := cfg.ValidatePlugins()
	if err == nil || !strings.Contains(err.Error(), "does-not-exist") {
		t.Errorf("expected unresolved-exclude error, got %v", err)
	}
}

func TestContextPluginsOverrideOnly(t *testing.T) {
	y := `
plugins:
  a/b: [b]
  c/d: [d]
contexts:
  ci:
    agent: claude
    plugins:
      only:
        - a/b
`
	var cfg config.Config
	if err := yaml.NewDecoder(strings.NewReader(y)).Decode(&cfg); err != nil {
		t.Fatal(err)
	}
	override := cfg.Contexts["ci"].Plugins
	if len(override.Only) != 1 || override.Only[0] != "a/b" {
		t.Errorf("only = %v", override.Only)
	}
}

func TestValidateHooksUnknownNameReturnsError(t *testing.T) {
	y := `
hooks:
  pre_tool:
    - command: global-guard
      name: guard
contexts:
  personal:
    agent: claude
    hooks:
      exclude_hooks:
        pre_tool: [typo-name]
`
	var cfg config.Config
	if err := yaml.Unmarshal([]byte(y), &cfg); err != nil {
		t.Fatal(err)
	}
	err := cfg.ValidateHooks()
	if err == nil {
		t.Fatal("expected error for unknown hook name, got nil")
	}
	if !strings.Contains(err.Error(), "typo-name") {
		t.Errorf("error should mention the bad name, got: %v", err)
	}
	if !strings.Contains(err.Error(), "personal") {
		t.Errorf("error should mention the context name, got: %v", err)
	}
}

func TestValidateHooksKnownNamePasses(t *testing.T) {
	y := `
hooks:
  pre_tool:
    - command: global-guard
      name: guard
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
	if err := yaml.Unmarshal([]byte(y), &cfg); err != nil {
		t.Fatal(err)
	}
	if err := cfg.ValidateHooks(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateHooksExtraNamePasses(t *testing.T) {
	y := `
hooks:
  pre_tool:
    - command: global-guard
      name: guard
contexts:
  work:
    agent: claude
    hooks:
      extra:
        pre_tool:
          - command: work-hook
            name: work-audit
      exclude_hooks:
        pre_tool: [work-audit]
`
	var cfg config.Config
	if err := yaml.Unmarshal([]byte(y), &cfg); err != nil {
		t.Fatal(err)
	}
	if err := cfg.ValidateHooks(); err != nil {
		t.Errorf("unexpected error for extra-declared name: %v", err)
	}
}

func TestValidateHooksUnnamedHooksIgnored(t *testing.T) {
	y := `
hooks:
  pre_tool:
    - command: unnamed-hook
contexts:
  personal:
    agent: claude
    hooks:
      exclude_hooks:
        pre_tool: [guard]
`
	var cfg config.Config
	if err := yaml.Unmarshal([]byte(y), &cfg); err != nil {
		t.Fatal(err)
	}
	err := cfg.ValidateHooks()
	if err == nil || !strings.Contains(err.Error(), "guard") {
		t.Errorf("expected error for 'guard' not found among named hooks, got: %v", err)
	}
}
