package gemini_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/jskswamy/aide/internal/provision"
	"github.com/jskswamy/aide/internal/provision/agents/gemini"
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
	call := append([]string{name}, args...)
	f.calls = append(f.calls, call)
	return f.stdout, f.stderr, f.code, f.err
}

func TestGeminiMarketplaceMethodsNoOp(t *testing.T) {
	r := &fakeRunner{}
	d := gemini.New(r)
	got, err := d.InstalledMarketplaces(provision.Context{})
	if err != nil || len(got) != 0 {
		t.Errorf("Gemini InstalledMarketplaces should be no-op, got %v, %v", got, err)
	}
	if err := d.RemoveMarketplace(provision.Context{}, "anything"); err != nil {
		t.Errorf("Gemini RemoveMarketplace should be no-op, got %v", err)
	}
}

func TestGeminiCapabilities(t *testing.T) {
	d := gemini.New(&fakeRunner{})
	if d.Name() != "gemini" {
		t.Errorf("Name = %q", d.Name())
	}
	if !d.SupportsPlugins() || !d.SupportsMCP() {
		t.Error("Gemini should support plugins and MCP")
	}
	if d.RequiresTTY() {
		t.Error("Gemini should not require TTY")
	}
	shapes := d.SupportedSourceShapes()
	if len(shapes) != 1 || shapes[0] != provision.ShapeURLDirect {
		t.Errorf("Gemini shapes = %v, want [url-direct]", shapes)
	}
}

func TestGeminiMCPConfigPath(t *testing.T) {
	d := gemini.New(&fakeRunner{})
	got := d.MCPConfigPath(provision.Context{HomeDir: "/home/u"})
	want := "/home/u/.gemini/settings.json"
	if got != want {
		t.Errorf("path = %q, want %q", got, want)
	}
}

func TestGeminiInstallPluginMarketplace(t *testing.T) {
	r := &fakeRunner{}
	d := gemini.New(r)
	err := d.InstallPlugin(provision.Context{}, provision.Plugin{Key: "x", Source: "marketplace", Name: "owner/repo"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"gemini", "extensions", "install", "owner/repo"}
	if !reflect.DeepEqual(r.calls[0], want) {
		t.Errorf("call = %v, want %v", r.calls[0], want)
	}
}

func TestGeminiInstallPluginLocal(t *testing.T) {
	r := &fakeRunner{}
	d := gemini.New(r)
	err := d.InstallPlugin(provision.Context{}, provision.Plugin{Key: "x", Source: "local", Name: "/abs/path"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"gemini", "extensions", "install", "--path", "/abs/path"}
	if !reflect.DeepEqual(r.calls[0], want) {
		t.Errorf("call = %v, want %v", r.calls[0], want)
	}
}

func TestGeminiInstallPluginGitURL(t *testing.T) {
	r := &fakeRunner{}
	d := gemini.New(r)
	err := d.InstallPlugin(provision.Context{}, provision.Plugin{Key: "x", Source: "git", Name: "https://github.com/o/r"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"gemini", "extensions", "install", "https://github.com/o/r"}
	if !reflect.DeepEqual(r.calls[0], want) {
		t.Errorf("call = %v", r.calls[0])
	}
}

func TestGeminiInstallPluginFailure(t *testing.T) {
	r := &fakeRunner{code: 1, stderr: "boom"}
	d := gemini.New(r)
	err := d.InstallPlugin(provision.Context{}, provision.Plugin{Key: "x", Source: "marketplace", Name: "o/r"})
	if err == nil {
		t.Fatal("expected error on non-zero exit")
	}
}

func TestGeminiUninstallPlugin(t *testing.T) {
	r := &fakeRunner{}
	d := gemini.New(r)
	if err := d.UninstallPlugin(provision.Context{}, "foo"); err != nil {
		t.Fatal(err)
	}
	want := []string{"gemini", "extensions", "uninstall", "foo"}
	if !reflect.DeepEqual(r.calls[0], want) {
		t.Errorf("call = %v, want %v", r.calls[0], want)
	}
}

func TestGeminiUninstallMissingIsOK(t *testing.T) {
	r := &fakeRunner{code: 1, stderr: "extension 'foo' not installed"}
	d := gemini.New(r)
	if err := d.UninstallPlugin(provision.Context{}, "foo"); err != nil {
		t.Errorf("missing extension should be treated as success: %v", err)
	}
}

func TestGeminiInstalledPluginsParsesFixture(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "extensions_list.txt"))
	if err != nil {
		t.Fatal(err)
	}
	r := &fakeRunner{stdout: string(raw)}
	d := gemini.New(r)
	got, err := d.InstalledPlugins(provision.Context{})
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, p := range got {
		names = append(names, p.Name)
	}
	want := []string{"mcp-everything", "my-helper", "toolkit"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("names = %v, want %v", names, want)
	}
}
