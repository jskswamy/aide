package copilot_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/jskswamy/aide/internal/provision"
	"github.com/jskswamy/aide/internal/provision/agents/copilot"
)

type fakeRunner struct {
	stdout string
	stderr string
	code   int
	err    error
	calls  [][]string
}

func (f *fakeRunner) Run(_ context.Context, _ map[string]string, name string, args ...string) (string, string, int, error) {
	call := append([]string{name}, args...)
	f.calls = append(f.calls, call)
	return f.stdout, f.stderr, f.code, f.err
}

func TestCopilotCapabilities(t *testing.T) {
	d := copilot.New(&fakeRunner{})
	if d.Name() != "copilot" {
		t.Errorf("Name = %q", d.Name())
	}
	if !d.SupportsPlugins() || !d.SupportsMCP() {
		t.Error("Copilot should support plugins and MCP")
	}
	if d.RequiresTTY() {
		t.Error("Copilot should not require TTY")
	}
	shapes := d.SupportedSourceShapes()
	if len(shapes) != 1 || shapes[0] != provision.ShapeMarketplace {
		t.Errorf("Copilot shapes = %v, want [marketplace]", shapes)
	}
}

func TestCopilotMCPConfigPath(t *testing.T) {
	d := copilot.New(&fakeRunner{})
	got := d.MCPConfigPath(provision.Context{HomeDir: "/home/u"})
	want := "/home/u/.copilot/mcp-config.json"
	if got != want {
		t.Errorf("path = %q, want %q", got, want)
	}
}

func TestCopilotInstallPluginMarketplace(t *testing.T) {
	r := &fakeRunner{}
	d := copilot.New(r)
	err := d.InstallPlugin(provision.Context{}, provision.Plugin{Key: "linear", Source: "marketplace", Name: "linear@copilot-plugins"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"copilot", "plugin", "install", "linear@copilot-plugins"}
	if !reflect.DeepEqual(r.calls[0], want) {
		t.Errorf("call = %v", r.calls[0])
	}
}

func TestCopilotUninstallPlugin(t *testing.T) {
	r := &fakeRunner{}
	d := copilot.New(r)
	if err := d.UninstallPlugin(provision.Context{}, "linear"); err != nil {
		t.Fatal(err)
	}
	want := []string{"copilot", "plugin", "uninstall", "linear"}
	if !reflect.DeepEqual(r.calls[0], want) {
		t.Errorf("call = %v", r.calls[0])
	}
}

func TestCopilotUninstallMissingIsOK(t *testing.T) {
	r := &fakeRunner{code: 1, stderr: "plugin 'x' not installed"}
	d := copilot.New(r)
	if err := d.UninstallPlugin(provision.Context{}, "x"); err != nil {
		t.Errorf("missing plugin must be tolerated: %v", err)
	}
}

func TestCopilotInstalledPluginsParsesFixture(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "plugin_list.txt"))
	if err != nil {
		t.Fatal(err)
	}
	r := &fakeRunner{stdout: string(raw)}
	d := copilot.New(r)
	got, err := d.InstalledPlugins(provision.Context{})
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, p := range got {
		names = append(names, p.Name)
	}
	want := []string{"linear", "github-actions", "slack"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("names = %v, want %v", names, want)
	}
}

func TestCopilotInstallFailure(t *testing.T) {
	r := &fakeRunner{code: 1, stderr: "boom"}
	d := copilot.New(r)
	if err := d.InstallPlugin(provision.Context{}, provision.Plugin{Key: "x", Source: "marketplace", Name: "x@m"}); err == nil {
		t.Fatal("expected install error")
	}
}
