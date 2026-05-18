package codex_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/pelletier/go-toml/v2"

	"github.com/jskswamy/aide/internal/provision"
	"github.com/jskswamy/aide/internal/provision/agents/codex"
)

type stubRunner struct{}

func (stubRunner) Run(_ context.Context, _ map[string]string, _ string, _ ...string) (string, string, int, error) {
	return "", "", 0, nil
}

func setupHome(t *testing.T, initialTOML string) (home, configPath string) {
	t.Helper()
	home = t.TempDir()
	if err := os.Mkdir(filepath.Join(home, ".codex"), 0o700); err != nil {
		t.Fatal(err)
	}
	configPath = filepath.Join(home, ".codex", "config.toml")
	if initialTOML != "" {
		if err := os.WriteFile(configPath, []byte(initialTOML), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return
}

func TestCodexCapabilities(t *testing.T) {
	d := codex.New(stubRunner{})
	if d.Name() != "codex" {
		t.Errorf("Name = %q", d.Name())
	}
	if !d.SupportsPlugins() || !d.SupportsMCP() {
		t.Error("Codex should support plugins and MCP")
	}
	if d.RequiresTTY() {
		t.Error("Codex driver should NOT require TTY (uses TOML edits)")
	}
	shapes := d.SupportedSourceShapes()
	if len(shapes) != 1 || shapes[0] != provision.ShapeMarketplace {
		t.Errorf("Codex shapes = %v, want [marketplace]", shapes)
	}
}

func TestCodexInstallPluginTogglesEnabled(t *testing.T) {
	home, path := setupHome(t, "")
	d := codex.New(stubRunner{})
	err := d.InstallPlugin(provision.Context{HomeDir: home}, provision.Plugin{Key: "linear@m", Source: "marketplace", Name: "linear@m"})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	doc := map[string]any{}
	_ = toml.Unmarshal(raw, &doc)
	plugins, _ := doc["plugins"].(map[string]any)
	body, _ := plugins["linear@m"].(map[string]any)
	if enabled, _ := body["enabled"].(bool); !enabled {
		t.Errorf("plugin enabled = false, want true; doc=%v", doc)
	}
}

func TestCodexUninstallPluginDisables(t *testing.T) {
	body := `
[plugins."linear@m"]
enabled = true
`
	home, path := setupHome(t, body)
	d := codex.New(stubRunner{})
	if err := d.UninstallPlugin(provision.Context{HomeDir: home}, "linear@m"); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	doc := map[string]any{}
	_ = toml.Unmarshal(raw, &doc)
	plugins, _ := doc["plugins"].(map[string]any)
	bodyMap, _ := plugins["linear@m"].(map[string]any)
	if enabled, _ := bodyMap["enabled"].(bool); enabled {
		t.Errorf("plugin enabled = true after uninstall")
	}
}

func TestCodexInstalledPluginsLists(t *testing.T) {
	body := `
[plugins."a@m"]
enabled = true

[plugins."b@m"]
enabled = false

[plugins."c@m"]
enabled = true
`
	home, _ := setupHome(t, body)
	d := codex.New(stubRunner{})
	got, err := d.InstalledPlugins(provision.Context{HomeDir: home})
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, p := range got {
		names = append(names, p.Name)
	}
	sort.Strings(names)
	want := []string{"a@m", "c@m"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("names = %v, want %v", names, want)
	}
}

func TestCodexInstallPreservesOtherKeys(t *testing.T) {
	body := `
model = "gpt-5"

[plugins."other@m"]
enabled = true
`
	home, path := setupHome(t, body)
	d := codex.New(stubRunner{})
	err := d.InstallPlugin(provision.Context{HomeDir: home}, provision.Plugin{Key: "new@m", Source: "marketplace", Name: "new@m"})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	doc := map[string]any{}
	_ = toml.Unmarshal(raw, &doc)
	if doc["model"] != "gpt-5" {
		t.Errorf("top-level 'model' was lost: %v", doc["model"])
	}
	plugins, _ := doc["plugins"].(map[string]any)
	if _, ok := plugins["other@m"]; !ok {
		t.Error("existing plugin other@m must survive")
	}
	if _, ok := plugins["new@m"]; !ok {
		t.Error("new plugin not added")
	}
}

func TestCodexMCPConfigPath(t *testing.T) {
	d := codex.New(stubRunner{})
	got := d.MCPConfigPath(provision.Context{HomeDir: "/h"})
	want := "/h/.codex/config.toml"
	if got != want {
		t.Errorf("path = %q, want %q", got, want)
	}
}
