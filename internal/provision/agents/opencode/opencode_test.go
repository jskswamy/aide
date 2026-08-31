package opencode_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/jskswamy/aide/internal/provision"
	"github.com/jskswamy/aide/internal/provision/agents/opencode"
)

type fakeRunner struct{}

func (fakeRunner) Run(_ context.Context, _ map[string]string, name string, args ...string) (string, string, int, error) {
	return "", "", 0, nil
}

func TestOpenCodeCapabilities(t *testing.T) {
	d := opencode.New(fakeRunner{})
	if d.Name() != "opencode" {
		t.Errorf("Name = %q", d.Name())
	}
	if !d.SupportsPlugins() || !d.SupportsMCP() || !d.SupportsHooks() {
		t.Error("OpenCode should support plugins, MCP, and hooks")
	}
	if d.RequiresTTY() {
		t.Error("OpenCode should not require TTY")
	}
	shapes := d.SupportedSourceShapes()
	if len(shapes) != 1 || shapes[0] != provision.ShapeURLDirect {
		t.Errorf("OpenCode shapes = %v, want [url-direct]", shapes)
	}
}

func TestOpenCodeMCPConfigPath(t *testing.T) {
	d := opencode.New(fakeRunner{})
	ctx := provision.Context{HomeDir: "/home/u"}
	want := filepath.Join("/home/u", ".config", "opencode", "opencode.jsonc")
	if got := d.MCPConfigPath(ctx); got != want {
		t.Errorf("MCPConfigPath = %q, want %q", got, want)
	}
	if d.MCPHandler(ctx) == nil {
		t.Error("MCPHandler should not be nil — OpenCode is file-edit, not CLI-driven")
	}
}

func TestOpenCodeConfigPathRespectsOpencodeConfigEnvVar(t *testing.T) {
	d := opencode.New(fakeRunner{})
	ctx := provision.Context{
		HomeDir: "/home/u",
		Env:     map[string]string{"OPENCODE_CONFIG": "/custom/opencode.jsonc"},
	}
	want := "/custom/opencode.jsonc"
	if got := d.MCPConfigPath(ctx); got != want {
		t.Errorf("MCPConfigPath = %q, want %q", got, want)
	}
}

func TestOpenCodeInstallPluginWritesArrayAndDedups(t *testing.T) {
	home := t.TempDir()
	ctx := provision.Context{HomeDir: home}
	d := opencode.New(fakeRunner{})

	if err := d.InstallPlugin(ctx, provision.Plugin{Key: "foo", Name: "my-npm-plugin"}); err != nil {
		t.Fatal(err)
	}
	// Installing the same ref again must not duplicate it.
	if err := d.InstallPlugin(ctx, provision.Plugin{Key: "foo", Name: "my-npm-plugin"}); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(filepath.Join(home, ".config", "opencode", "opencode.jsonc"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Plugin []string `json:"plugin"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(doc.Plugin, []string{"my-npm-plugin"}) {
		t.Errorf("plugin array = %v, want [my-npm-plugin]", doc.Plugin)
	}
}

func TestOpenCodeInstalledPluginsParsesArray(t *testing.T) {
	home := t.TempDir()
	ctx := provision.Context{HomeDir: home}
	d := opencode.New(fakeRunner{})

	if err := d.InstallPlugin(ctx, provision.Plugin{Key: "a", Name: "plugin-a"}); err != nil {
		t.Fatal(err)
	}
	if err := d.InstallPlugin(ctx, provision.Plugin{Key: "b", Name: "plugin-b"}); err != nil {
		t.Fatal(err)
	}

	got, err := d.InstalledPlugins(ctx)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, p := range got {
		names = append(names, p.Name)
	}
	if len(names) != 2 {
		t.Fatalf("names = %v, want 2 entries", names)
	}
}

func TestOpenCodeUninstallPluginRemoves(t *testing.T) {
	home := t.TempDir()
	ctx := provision.Context{HomeDir: home}
	d := opencode.New(fakeRunner{})

	_ = d.InstallPlugin(ctx, provision.Plugin{Key: "a", Name: "plugin-a"})
	_ = d.InstallPlugin(ctx, provision.Plugin{Key: "b", Name: "plugin-b"})
	if err := d.UninstallPlugin(ctx, "plugin-a"); err != nil {
		t.Fatal(err)
	}

	got, err := d.InstalledPlugins(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "plugin-b" {
		t.Errorf("InstalledPlugins after uninstall = %+v", got)
	}
}

func TestOpenCodeUninstallMissingIsNoOp(t *testing.T) {
	home := t.TempDir()
	ctx := provision.Context{HomeDir: home}
	d := opencode.New(fakeRunner{})
	if err := d.UninstallPlugin(ctx, "never-installed"); err != nil {
		t.Errorf("uninstalling an absent plugin should be a no-op, got %v", err)
	}
}

func TestOpenCodeInstalledPluginsEmptyWhenConfigMissing(t *testing.T) {
	home := t.TempDir()
	ctx := provision.Context{HomeDir: home}
	d := opencode.New(fakeRunner{})
	got, err := d.InstalledPlugins(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty, got %+v", got)
	}
}

func TestOpenCodeMarketplaceMethodsNoOp(t *testing.T) {
	d := opencode.New(fakeRunner{})
	got, err := d.InstalledMarketplaces(provision.Context{})
	if err != nil || len(got) != 0 {
		t.Errorf("InstalledMarketplaces should be no-op, got %v, %v", got, err)
	}
	if err := d.RemoveMarketplace(provision.Context{}, "anything"); err != nil {
		t.Errorf("RemoveMarketplace should be no-op, got %v", err)
	}
	if err := d.AddMarketplace(provision.Context{}, provision.Marketplace{}); err == nil {
		t.Error("AddMarketplace should error — OpenCode has no marketplace concept")
	}
}
