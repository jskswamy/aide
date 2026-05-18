package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/provision"
	"github.com/jskswamy/aide/internal/provision/provisiontest"
)

// fakeProv wraps the shared FakeProvisioner with a local MCP-state
// shim. The base behavior (capabilities, error injection, call
// recording) comes from provisiontest.FakeProvisioner; this wrapper
// adds the locally-needed mcpInstalled map that feeds fakeMCPHandler.
type fakeProv struct {
	*provisiontest.FakeProvisioner
	mcpInstalled map[string]provision.MCPServer
}

// MCPHandler overrides the base no-op handler to return a
// fakeMCPHandler seeded from the wrapper's mcpInstalled map. Tests
// set mcpInstalled before calling cmds; the engine then reads
// back through this handler.
func (f *fakeProv) MCPHandler(_ provision.Context) provision.MCPHandler {
	return &fakeMCPHandler{servers: f.mcpInstalled}
}

var theFakeProv = &fakeProv{
	FakeProvisioner: &provisiontest.FakeProvisioner{
		AgentName:      "fakeagent",
		SupportsPlug:   true,
		SupportsMCPCfg: true,
		Shapes:         []provision.SourceShape{provision.ShapeMarketplace},
		MCPPath:        "/tmp/fakeagent-mcp.json",
	},
}

type fakeMCPHandler struct {
	servers map[string]provision.MCPServer
	managed map[string]bool
	writes  []map[string]provision.MCPServer
	writeFn func(string, map[string]provision.MCPServer) error
}

func (h *fakeMCPHandler) Read(_ string) (map[string]provision.MCPServer, map[string]bool, error) {
	out := map[string]provision.MCPServer{}
	for k, v := range h.servers {
		out[k] = v
	}
	mgd := map[string]bool{}
	for k, v := range h.managed {
		mgd[k] = v
	}
	return out, mgd, nil
}

func (h *fakeMCPHandler) Write(_ string, desired map[string]provision.MCPServer) error {
	cp := map[string]provision.MCPServer{}
	for k, v := range desired {
		cp[k] = v
	}
	h.writes = append(h.writes, cp)
	h.servers = cp
	if h.writeFn != nil {
		return h.writeFn("", desired)
	}
	return nil
}

func init() {
	provision.RegisterProvisioner(theFakeProv)
}

// fakeProvReset clears recorded state and per-test data while
// preserving the agent identity/capability fields the registry knows
// about.
func fakeProvReset(t *testing.T) {
	t.Helper()
	theFakeProv.Reset()
	theFakeProv.mcpInstalled = nil
}

// setupProvisionConfig writes a config.yaml that registers a "work"
// context bound to the current cwd and using agent "fakeagent". It
// returns the test home dir.
func setupProvisionConfig(t *testing.T, plugins []string, mcpServers []string, pluginDecls map[string]string, mcpDecls map[string]string) string {
	t.Helper()
	dir := isolatedConfigDir(t)

	// v2 schema: plugins are polymorphic. Tests historically declared
	// each plugin via { source: marketplace, name: <ref> }; emit the
	// new url-direct shape (string value at a plain key) and ignore
	// the per-context plugins selection list (in v2 the top-level
	// declaration drives desired set; per-context override is optional).
	var b strings.Builder
	if len(pluginDecls) > 0 {
		b.WriteString("plugins:\n")
		for name, src := range pluginDecls {
			b.WriteString("  ")
			b.WriteString(name)
			b.WriteString(": \"")
			b.WriteString(src)
			b.WriteString("\"\n")
		}
	}
	if len(mcpDecls) > 0 {
		b.WriteString("mcp_servers:\n")
		for name, cmd := range mcpDecls {
			b.WriteString("  ")
			b.WriteString(name)
			b.WriteString(":\n    command: ")
			b.WriteString(cmd)
			b.WriteString("\n")
		}
	}
	b.WriteString("contexts:\n  work:\n    agent: fakeagent\n    match:\n      - path: ")
	cwd, _ := os.Getwd()
	b.WriteString(cwd)
	b.WriteString("\n")
	_ = plugins // v2: top-level declaration drives desired set
	if len(mcpServers) > 0 {
		b.WriteString("    mcp_servers:\n")
		for _, m := range mcpServers {
			b.WriteString("      - ")
			b.WriteString(m)
			b.WriteString("\n")
		}
	}
	path := filepath.Join(dir, "xdg", "aide", "config.yaml")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// writeStateFile writes a managed-state JSON with the given managed
// plugins / MCP servers under context "work". Returns nothing; the
// state path is the default for the test HOME.
func writeStateFile(t *testing.T, homeDir string, plugins, mcps []string) {
	t.Helper()
	st := &provision.ManagedState{
		Version:  provision.StateVersion,
		Contexts: map[string]*provision.ContextState{},
	}
	cs := &provision.ContextState{
		Plugins:    map[string]provision.ManagedItem{},
		MCPServers: map[string]provision.ManagedItem{},
	}
	for _, p := range plugins {
		cs.Plugins[p] = provision.ManagedItem{}
	}
	for _, m := range mcps {
		cs.MCPServers[m] = provision.ManagedItem{}
	}
	st.Contexts["work"] = cs
	if err := provision.SaveState(provision.DefaultStatePath(homeDir), st); err != nil {
		t.Fatal(err)
	}
}

func TestPluginList_DeclaredInstalledManaged(t *testing.T) {
	fakeProvReset(t)
	home := setupProvisionConfig(t,
		[]string{"linear", "github"},
		nil,
		map[string]string{"linear": "linear@1.2", "github": "github"},
		nil,
	)
	theFakeProv.InstalledPluginList = []provision.Plugin{
		{Key: "linear"}, {Key: "github"}, {Key: "experimental"},
	}
	writeStateFile(t, home, []string{"linear", "github", "old-tool"}, nil)

	var buf bytes.Buffer
	cmd := pluginListCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--context", "work"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v\n%s", err, buf.String())
	}
	out := buf.String()
	for _, want := range []string{
		"Context: work",
		"agent: fakeagent",
		"linear",
		"marketplace linear@1.2",
		"github",
		"experimental",
		"unmanaged",
		"old-tool",
		"stale managed",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in output:\n%s", want, out)
		}
	}
}

// TestPluginList_ShowsMarketplaceColumn verifies that for marketplace
// agents (SupportedSourceShapes contains ShapeMarketplace), the
// rendered output includes a marketplaces section with declared,
// installed, and managed columns just like plugins. Surfaced as
// follow-up: installed-but-not-declared marketplaces should appear
// in the table as "unmanaged".
func TestPluginList_ShowsMarketplaceColumn(t *testing.T) {
	fakeProvReset(t)
	dir := isolatedConfigDir(t)
	cwd, _ := os.Getwd()
	body := "plugins:\n" +
		"  steveyegge/beads:\n" +
		"    - beads\n" +
		"  jskswamy/claude-plugins:\n" +
		"    - craft\n" +
		"contexts:\n" +
		"  work:\n" +
		"    agent: fakeagent\n" +
		"    match:\n" +
		"      - path: " + cwd + "\n"
	if err := os.WriteFile(filepath.Join(dir, "xdg", "aide", "config.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	theFakeProv.InstalledMarkets = []provision.Marketplace{
		{Key: "steveyegge/beads", Source: "github:steveyegge/beads", Name: "beads-marketplace"},
		{Key: "extra-org/extra-marketplace", Source: "github:extra-org/extra-marketplace", Name: "extra-marketplace"},
	}

	var buf bytes.Buffer
	cmd := pluginListCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--context", "work"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v\n%s", err, buf.String())
	}
	out := buf.String()
	for _, want := range []string{
		"MARKETPLACES",                // section header
		"steveyegge/beads",            // declared and installed → ✓ ✓
		"jskswamy/claude-plugins",     // declared, not installed → ✓ —
		"extra-org/extra-marketplace", // installed but not declared → unmanaged
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in marketplace section:\n%s", want, out)
		}
	}
}

func TestMCPList_DeclaredInstalledManaged(t *testing.T) {
	fakeProvReset(t)
	home := setupProvisionConfig(t,
		nil,
		[]string{"shared"},
		nil,
		map[string]string{"shared": "shared-mcp"},
	)
	theFakeProv.mcpInstalled = map[string]provision.MCPServer{
		"shared":    {Command: "shared-mcp"},
		"extra-mcp": {Command: "extra"},
	}
	writeStateFile(t, home, nil, []string{"shared", "stale-mcp"})

	var buf bytes.Buffer
	cmd := mcpListCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--context", "work"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v\n%s", err, buf.String())
	}
	out := buf.String()
	for _, want := range []string{
		"Context: work",
		"agent: fakeagent",
		"shared",
		"shared-mcp",
		"extra-mcp",
		"unmanaged",
		"stale-mcp",
		"stale managed",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in output:\n%s", want, out)
		}
	}
}
