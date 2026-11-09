package omp_test

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/jskswamy/aide/internal/provision"
	ompdriver "github.com/jskswamy/aide/internal/provision/agents/omp"
)

type fakeRunner struct {
	stdout string
	stderr string
	code   int
	err    error
	calls  [][]string
	envs   []map[string]string
}

func (f *fakeRunner) Run(_ context.Context, pctx provision.Context, name string, args ...string) (string, string, int, error) {
	f.calls = append(f.calls, append([]string{name}, args...))
	f.envs = append(f.envs, pctx.Env)
	return f.stdout, f.stderr, f.code, f.err
}

func TestOMPCapabilities(t *testing.T) {
	d := ompdriver.New(&fakeRunner{})
	if d.Name() != "omp" {
		t.Errorf("Name = %q", d.Name())
	}
	if !d.SupportsPlugins() || !d.SupportsMCP() || !d.SupportsHooks() {
		t.Error("OMP should support plugins, MCP, and hooks")
	}
	if d.RequiresTTY() {
		t.Error("OMP should not require TTY")
	}
	if d.PluginsByInstallRef() {
		t.Error("OMP plugins are key-addressed, not install-ref-addressed")
	}
	shapes := d.SupportedSourceShapes()
	if len(shapes) != 1 || shapes[0] != provision.ShapeURLDirect {
		t.Errorf("OMP shapes = %v, want [url-direct]", shapes)
	}
}

func TestOMPAgentDirAndMCPConfig(t *testing.T) {
	d := ompdriver.New(&fakeRunner{})
	ctx := provision.Context{HomeDir: "/home/u"}
	if got, want := d.AgentDir(ctx), filepath.Join("/home/u", ".omp", "agent"); got != want {
		t.Errorf("AgentDir = %q, want %q", got, want)
	}
	if got, want := d.MCPConfigPath(ctx), filepath.Join("/home/u", ".omp", "agent", "mcp.json"); got != want {
		t.Errorf("MCPConfigPath = %q, want %q", got, want)
	}

	override := provision.Context{HomeDir: "/home/u", Env: map[string]string{"PI_CODING_AGENT_DIR": "/home/u/.omp-work"}}
	if got, want := d.AgentDir(override), "/home/u/.omp-work"; got != want {
		t.Errorf("AgentDir(override) = %q, want %q", got, want)
	}
	if got, want := d.MCPConfigPath(override), filepath.Join("/home/u/.omp-work", "mcp.json"); got != want {
		t.Errorf("MCPConfigPath(override) = %q, want %q", got, want)
	}
}

func TestOMPMCPRoundTrip(t *testing.T) {
	d := ompdriver.New(&fakeRunner{})
	ctx := provision.Context{HomeDir: t.TempDir()}
	path := d.MCPConfigPath(ctx)
	h := d.MCPHandler(ctx)
	want := provision.MCPServer{Key: "cbm", Command: "codebase-memory-mcp", Env: map[string]string{"A": "b"}}
	if err := h.Write(path, map[string]provision.MCPServer{"cbm": want}); err != nil {
		t.Fatal(err)
	}
	got, managed, err := h.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got["cbm"], want) || !managed["cbm"] {
		t.Errorf("Read = %+v managed=%v, want %+v managed", got["cbm"], managed, want)
	}
}

func TestOMPPluginInstallUninstallAndList(t *testing.T) {
	r := &fakeRunner{}
	d := ompdriver.New(r)
	ctx := provision.Context{
		HomeDir: "/home/u",
		Env:     map[string]string{"PI_CODING_AGENT_DIR": "/home/u/.omp-work"},
	}

	if err := d.InstallPlugin(ctx, provision.Plugin{Key: "demo", Name: "npm:@scope/demo"}); err != nil {
		t.Fatal(err)
	}
	if want := []string{"omp", "install", "@scope/demo"}; !reflect.DeepEqual(r.calls[0], want) {
		t.Errorf("install call = %v, want %v", r.calls[0], want)
	}
	if got := r.envs[0]["PI_CONFIG_DIR"]; got != ".omp-work" {
		t.Errorf("PI_CONFIG_DIR = %q, want .omp-work", got)
	}

	// Uninstall tolerates "not installed" on stderr for rollback idempotency.
	r.code = 1
	r.stderr = "✘ demo is not installed"
	if err := d.UninstallPlugin(ctx, "demo"); err != nil {
		t.Fatalf("UninstallPlugin(not installed) = %v, want nil", err)
	}

	// List parses both npm and marketplace sections.
	r.code = 0
	r.stderr = ""
	r.stdout = `{"npm":[{"name":"demo","version":"1.0.0","enabled":true}],"marketplace":[{"id":"safety@official"}]}`
	got, err := d.InstalledPlugins(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := []provision.Plugin{
		{Key: "demo", Name: "demo"},
		{Key: "safety", Source: "marketplace", Name: "safety@official"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("InstalledPlugins = %+v, want %+v", got, want)
	}
}
