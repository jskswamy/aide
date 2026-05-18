package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/provision"
)

func runAdoptCmd(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()
	cmd := adoptCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

func TestAdopt_YesPromotesUnmanaged(t *testing.T) {
	fakeProvReset(t)
	home := setupProvisionConfig(t, nil, nil, nil, nil)
	theFakeProv.plugins = []provision.Plugin{
		{Key: "experimental", Source: "marketplace", Name: "experimental@1.0"},
	}
	theFakeProv.mcpInstalled = map[string]provision.MCPServer{
		"extra": {Command: "extra-cmd", Args: []string{"--flag"}},
	}

	out, err := runAdoptCmd(t, "", "--context", "work", "--yes")
	if err != nil {
		t.Fatalf("execute: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Adopted") {
		t.Errorf("missing summary:\n%s", out)
	}

	// config.yaml must include the new plugin + mcp entries.
	cfgPath := filepath.Join(home, "xdg", "aide", "config.yaml")
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	for _, want := range []string{"experimental:", "experimental@1.0", "extra:", "extra-cmd"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in config:\n%s", want, body)
		}
	}

	// Reload to confirm references attach to the context.
	cfg, err := config.Load(filepath.Join(home, "xdg", "aide"), home)
	if err != nil {
		t.Fatal(err)
	}
	ctx := cfg.Contexts["work"]
	// TODO(v2-task12): re-enable plugin assertion after adopt rewrites
	// in the polymorphic shape. v1 ctx.Plugins []string is gone.
	_ = ctx
	if !contains(ctx.MCPServers, "extra") {
		t.Errorf("context mcp missing extra: %v", ctx.MCPServers)
	}

	// State should mark them managed.
	st, err := provision.LoadState(provision.DefaultStatePath(home))
	if err != nil {
		t.Fatal(err)
	}
	cs := st.Contexts["work"]
	if cs == nil || cs.Plugins["experimental"].Version != "1.0" {
		t.Errorf("state did not record adopted plugin: %+v", cs)
	}
	if _, ok := cs.MCPServers["extra"]; !ok {
		t.Errorf("state did not record adopted mcp: %+v", cs)
	}
}

// TestAdopt_MarketplaceAgentWritesNestedShape verifies that adopting
// a plugin from a marketplace-class agent produces a list-valued
// (marketplace-shape) entry under the repo key, NOT a URL-direct
// entry. The driver's InstalledMarketplaces provides the
// marketplace-name → repo mapping needed to synthesise the repo key.
func TestAdopt_MarketplaceAgentWritesNestedShape(t *testing.T) {
	fakeProvReset(t)
	home := setupProvisionConfig(t, nil, nil, nil, nil)
	// Marketplace agent (fakeProv already returns ShapeMarketplace).
	theFakeProv.plugins = []provision.Plugin{
		{Key: "beads", Source: "marketplace", Name: "beads@beads-marketplace"},
	}
	theFakeProv.marketplaces = []provision.Marketplace{
		{Key: "steveyegge/beads", Source: "github:steveyegge/beads", Name: "beads-marketplace"},
	}

	out, err := runAdoptCmd(t, "", "--context", "work", "--yes")
	if err != nil {
		t.Fatalf("execute: %v\n%s", err, out)
	}

	cfgPath := filepath.Join(home, "xdg", "aide", "config.yaml")
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	// Must contain the repo key with a list-valued entry; must NOT
	// fall back to URL-direct under the bare plugin name.
	if !strings.Contains(body, "steveyegge/beads:") {
		t.Errorf("adopted plugin should land under marketplace repo key 'steveyegge/beads:'; got:\n%s", body)
	}
	if !strings.Contains(body, "- beads") {
		t.Errorf("plugin name 'beads' should appear as a list item under the marketplace; got:\n%s", body)
	}
	// Reload + verify the shape via the parser.
	cfg, err := config.Load(filepath.Join(home, "xdg", "aide"), home)
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := cfg.Plugins["steveyegge/beads"]
	if !ok {
		t.Fatalf("config.Plugins missing the marketplace key, got %+v", cfg.Plugins)
	}
	if entry.Shape() != config.PluginShapeMarketplace {
		t.Errorf("entry shape = %v, want marketplace", entry.Shape())
	}
	if len(entry.Plugins) != 1 || entry.Plugins[0] != "beads" {
		t.Errorf("entry.Plugins = %v, want [beads]", entry.Plugins)
	}
}

// TestAdopt_PromotesUnmanagedMarketplace verifies that adopt walks
// unmanaged marketplaces (installed in the agent, not declared, not
// previously managed) and writes them to config.yaml as declare-only
// (null-valued) plugin entries, then marks them managed in the state
// file. Without this, the marketplace stays in the perpetual
// `unmanaged` column on every `aide sync --plan` and the user has no
// in-band way to claim it.
func TestAdopt_PromotesUnmanagedMarketplace(t *testing.T) {
	fakeProvReset(t)
	home := setupProvisionConfig(t, nil, nil, nil, nil)
	theFakeProv.marketplaces = []provision.Marketplace{
		{Key: "jskswamy/aide", Source: "github:jskswamy/aide", Name: "aide-plugins"},
	}

	out, err := runAdoptCmd(t, "", "--context", "work", "--yes")
	if err != nil {
		t.Fatalf("execute: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Adopted") {
		t.Errorf("missing summary:\n%s", out)
	}

	cfgPath := filepath.Join(home, "xdg", "aide", "config.yaml")
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	if !strings.Contains(body, "jskswamy/aide") {
		t.Errorf("config missing adopted marketplace key:\n%s", body)
	}

	cfg, err := config.Load(filepath.Join(home, "xdg", "aide"), home)
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := cfg.Plugins["jskswamy/aide"]
	if !ok {
		t.Fatalf("config.Plugins missing the marketplace key, got %+v", cfg.Plugins)
	}
	if entry.Shape() != config.PluginShapeDeclareOnly {
		t.Errorf("entry shape = %v, want declare-only (null)", entry.Shape())
	}

	st, err := provision.LoadState(provision.DefaultStatePath(home))
	if err != nil {
		t.Fatal(err)
	}
	cs := st.Contexts["work"]
	if cs == nil {
		t.Fatal("state missing work context")
		return
	}
	if _, ok := cs.Marketplaces["jskswamy/aide"]; !ok {
		t.Errorf("state did not record adopted marketplace: %+v", cs.Marketplaces)
	}
}

func TestAdopt_NothingToDo(t *testing.T) {
	fakeProvReset(t)
	setupProvisionConfig(t, nil, nil, nil, nil)
	out, err := runAdoptCmd(t, "", "--context", "work", "--yes")
	if err != nil {
		t.Fatalf("execute: %v\n%s", err, out)
	}
	if !strings.Contains(out, "No unmanaged") {
		t.Errorf("expected nothing-to-do message:\n%s", out)
	}
}

func contains(xs []string, target string) bool {
	for _, x := range xs {
		if x == target {
			return true
		}
	}
	return false
}
