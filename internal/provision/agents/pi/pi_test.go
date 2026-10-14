package pi_test

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/jskswamy/aide/internal/provision"
	pidriver "github.com/jskswamy/aide/internal/provision/agents/pi"
)

// fakeRunner records calls and returns scripted output.
type fakeRunner struct {
	stdout string
	stderr string
	code   int
	err    error
	calls  [][]string
}

func (f *fakeRunner) Run(_ context.Context, _ map[string]string, name string, args ...string) (string, string, int, error) {
	f.calls = append(f.calls, append([]string{name}, args...))
	return f.stdout, f.stderr, f.code, f.err
}

func TestPiCapabilities(t *testing.T) {
	d := pidriver.New(&fakeRunner{})
	if d.Name() != "pi" {
		t.Errorf("Name = %q", d.Name())
	}
	if !d.SupportsPlugins() || !d.SupportsMCP() || !d.SupportsHooks() {
		t.Error("Pi should support plugins, MCP, and hooks")
	}
	if d.RequiresTTY() {
		t.Error("Pi should not require TTY")
	}
	shapes := d.SupportedSourceShapes()
	if len(shapes) != 1 || shapes[0] != provision.ShapeURLDirect {
		t.Errorf("Pi shapes = %v, want [url-direct]", shapes)
	}
}

func TestPiAgentDirDefault(t *testing.T) {
	d := pidriver.New(&fakeRunner{})
	ctx := provision.Context{HomeDir: "/home/u"}
	want := filepath.Join("/home/u", ".pi", "agent")
	if got := d.AgentDir(ctx); got != want {
		t.Errorf("AgentDir = %q, want %q", got, want)
	}
}

func TestPiAgentDirEnvOverride(t *testing.T) {
	d := pidriver.New(&fakeRunner{})
	ctx := provision.Context{HomeDir: "/home/u", Env: map[string]string{"PI_CODING_AGENT_DIR": "/custom/pi-agent"}}
	if got := d.AgentDir(ctx); got != "/custom/pi-agent" {
		t.Errorf("AgentDir = %q, want /custom/pi-agent", got)
	}
}

func TestPiMCPConfigPath(t *testing.T) {
	d := pidriver.New(&fakeRunner{})
	ctx := provision.Context{HomeDir: "/home/u"}
	want := filepath.Join("/home/u", ".pi", "agent", "mcp.json")
	if got := d.MCPConfigPath(ctx); got != want {
		t.Errorf("MCPConfigPath = %q, want %q", got, want)
	}
	if d.MCPHandler(ctx) == nil {
		t.Error("MCPHandler should not be nil")
	}
}

func TestPiMCPConfigPathEnvOverride(t *testing.T) {
	d := pidriver.New(&fakeRunner{})
	ctx := provision.Context{HomeDir: "/home/u", Env: map[string]string{"PI_CODING_AGENT_DIR": "/custom/pi-agent"}}
	want := filepath.Join("/custom/pi-agent", "mcp.json")
	if got := d.MCPConfigPath(ctx); got != want {
		t.Errorf("MCPConfigPath = %q, want %q", got, want)
	}
}

// TestPiMCPRoundTrip pins that Pi's mcp.json uses the standard flat
// mcpServers shape (confirmed against pi 1.1.0's docs/mcp.md): a
// server written through the handler reads back unchanged.
func TestPiMCPRoundTrip(t *testing.T) {
	d := pidriver.New(&fakeRunner{})
	ctx := provision.Context{HomeDir: t.TempDir()}
	path := d.MCPConfigPath(ctx)
	h := d.MCPHandler(ctx)
	want := provision.MCPServer{Key: "fs", Command: "npx", Args: []string{"-y", "srv"}, Env: map[string]string{"A": "b"}}
	if err := h.Write(path, map[string]provision.MCPServer{"fs": want}); err != nil {
		t.Fatal(err)
	}
	got, managed, err := h.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got["fs"], want) || !managed["fs"] {
		t.Errorf("Read = %+v managed=%v, want %+v managed", got["fs"], managed, want)
	}
}

func TestPiInstallPlugin(t *testing.T) {
	r := &fakeRunner{}
	d := pidriver.New(r)
	if err := d.InstallPlugin(provision.Context{}, provision.Plugin{Key: "x", Name: "npm:@foo/bar"}); err != nil {
		t.Fatal(err)
	}
	want := []string{"pi", "install", "npm:@foo/bar", "--no-approve"}
	if !reflect.DeepEqual(r.calls[0], want) {
		t.Errorf("call = %v, want %v", r.calls[0], want)
	}
}

func TestPiInstallPluginFailure(t *testing.T) {
	r := &fakeRunner{code: 1, stderr: "boom"}
	d := pidriver.New(r)
	if err := d.InstallPlugin(provision.Context{}, provision.Plugin{Key: "x", Name: "npm:@foo/bar"}); err == nil {
		t.Fatal("expected error on non-zero exit")
	}
}

func TestPiUninstallPlugin(t *testing.T) {
	r := &fakeRunner{}
	d := pidriver.New(r)
	if err := d.UninstallPlugin(provision.Context{}, "npm:@foo/bar"); err != nil {
		t.Fatal(err)
	}
	want := []string{"pi", "remove", "npm:@foo/bar", "--no-approve"}
	if !reflect.DeepEqual(r.calls[0], want) {
		t.Errorf("call = %v, want %v", r.calls[0], want)
	}
}

// TestPiUninstallMissingIsOK pins the tolerated-stderr fix: Pi's own
// wording ("No matching package found for X") does NOT contain any
// substring in provision.DefaultTolerateStderr ("not installed", "not
// found", "not configured") — confirmed 2026-08-31 (and re-confirmed against pi 1.1.0 on 2026-10-14) by running `pi
// remove` against a package that was never installed. Without the
// extra tolerated substring below, this test fails.
func TestPiUninstallMissingIsOK(t *testing.T) {
	r := &fakeRunner{code: 1, stderr: "No matching package found for npm:@nonexistent/pkg"}
	d := pidriver.New(r)
	if err := d.UninstallPlugin(provision.Context{}, "npm:@nonexistent/pkg"); err != nil {
		t.Errorf("missing package should be treated as success: %v", err)
	}
}

// TestPiInstalledPluginsParsesTwoLinePerEntryOutput pins the real `pi
// list` shape confirmed 2026-08-31 (and re-confirmed against pi 1.1.0 on 2026-10-14) by installing a real local
// extension against an isolated $HOME: each entry is TWO lines — a
// 2-space-indented declared source, then a 4-space-indented resolved
// absolute path. provision.ParsePluginList (one entry per line) can't
// parse this; the Pi driver needs its own two-line-aware parser.
func TestPiInstalledPluginsParsesTwoLinePerEntryOutput(t *testing.T) {
	stdout := "User packages:\n" +
		"  npm:@foo/bar\n" +
		"    /home/u/.pi/agent/packages/npm/@foo/bar\n" +
		"  ../../../dummy_ext\n" +
		"    /private/tmp/scratch/dummy_ext\n"
	r := &fakeRunner{stdout: stdout}
	d := pidriver.New(r)
	got, err := d.InstalledPlugins(provision.Context{})
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, p := range got {
		names = append(names, p.Name)
	}
	want := []string{"npm:@foo/bar", "../../../dummy_ext"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("names = %v, want %v", names, want)
	}
}

// TestPiInstalledPluginsEmptyState pins the real empty-state message
// confirmed 2026-08-31: `pi list` on a fresh $HOME prints exactly
// "No packages installed." (not the "NAME"/"No plugins" style header
// gemini/copilot use).
func TestPiInstalledPluginsEmptyState(t *testing.T) {
	r := &fakeRunner{stdout: "No packages installed.\n"}
	d := pidriver.New(r)
	got, err := d.InstalledPlugins(provision.Context{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty, got %+v", got)
	}
}

func TestPiInstalledPluginsBinaryMissing(t *testing.T) {
	r := &fakeRunner{err: context.DeadlineExceeded}
	d := pidriver.New(r)
	got, err := d.InstalledPlugins(provision.Context{})
	if err != nil || len(got) != 0 {
		t.Errorf("binary-missing should collapse to (nil, nil), got %v, %v", got, err)
	}
}

func TestPiMarketplaceMethodsNoOp(t *testing.T) {
	d := pidriver.New(&fakeRunner{})
	got, err := d.InstalledMarketplaces(provision.Context{})
	if err != nil || len(got) != 0 {
		t.Errorf("InstalledMarketplaces should be no-op, got %v, %v", got, err)
	}
	if err := d.RemoveMarketplace(provision.Context{}, "anything"); err != nil {
		t.Errorf("RemoveMarketplace should be no-op, got %v", err)
	}
	if err := d.AddMarketplace(provision.Context{}, provision.Marketplace{}); err == nil {
		t.Error("AddMarketplace should error — Pi has no marketplace concept")
	}
}
