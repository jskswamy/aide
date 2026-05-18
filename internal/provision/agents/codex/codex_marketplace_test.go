package codex_test

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/pelletier/go-toml/v2"

	"github.com/jskswamy/aide/internal/provision"
	"github.com/jskswamy/aide/internal/provision/agents/codex"
)

func TestCodexMCPHandler(t *testing.T) {
	d := codex.New(stubRunner{})
	if d.MCPHandler(provision.Context{}) == nil {
		t.Error("MCPHandler must not be nil")
	}
}

func TestCodexInstalledMarketplacesEmpty(t *testing.T) {
	home, _ := setupHome(t, "")
	d := codex.New(stubRunner{})
	got, err := d.InstalledMarketplaces(provision.Context{HomeDir: home})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty, got %+v", got)
	}
}

func TestCodexInstalledMarketplacesLists(t *testing.T) {
	body := `
[plugin_marketplaces.acme]
source = "github:acme/codex-marketplace"

[plugin_marketplaces.zeta]
source = "github:zeta/codex-marketplace"
`
	home, _ := setupHome(t, body)
	d := codex.New(stubRunner{})
	got, err := d.InstalledMarketplaces(provision.Context{HomeDir: home})
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, m := range got {
		names = append(names, m.Name)
	}
	sort.Strings(names)
	want := []string{"acme", "zeta"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("names = %v, want %v", names, want)
	}
	// source must round-trip
	for _, m := range got {
		if m.Source == "" {
			t.Errorf("marketplace %q has empty source", m.Name)
		}
	}
}

func TestCodexAddMarketplaceWritesEntry(t *testing.T) {
	home, path := setupHome(t, "")
	d := codex.New(stubRunner{})
	err := d.AddMarketplace(provision.Context{HomeDir: home}, provision.Marketplace{
		Key:    "acme/repo",
		Name:   "acme",
		Source: "github:acme/repo",
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	doc := map[string]any{}
	_ = toml.Unmarshal(raw, &doc)
	mks, _ := doc["plugin_marketplaces"].(map[string]any)
	body, _ := mks["acme"].(map[string]any)
	if body["source"] != "github:acme/repo" {
		t.Errorf("source = %v, want github:acme/repo", body["source"])
	}
}

func TestCodexAddMarketplaceFallsBackToKeyWhenNameAbsent(t *testing.T) {
	home, path := setupHome(t, "")
	d := codex.New(stubRunner{})
	err := d.AddMarketplace(provision.Context{HomeDir: home}, provision.Marketplace{
		Key:    "acme/repo",
		Source: "github:acme/repo",
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	doc := map[string]any{}
	_ = toml.Unmarshal(raw, &doc)
	mks, _ := doc["plugin_marketplaces"].(map[string]any)
	if _, ok := mks["acme/repo"]; !ok {
		t.Errorf("expected entry under key when name empty: %+v", mks)
	}
}

func TestCodexAddMarketplacePreservesOtherKeys(t *testing.T) {
	body := `
model = "gpt-5"

[plugin_marketplaces.existing]
source = "github:other/repo"
`
	home, path := setupHome(t, body)
	d := codex.New(stubRunner{})
	err := d.AddMarketplace(provision.Context{HomeDir: home}, provision.Marketplace{
		Name:   "new",
		Source: "github:new/repo",
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	doc := map[string]any{}
	_ = toml.Unmarshal(raw, &doc)
	if doc["model"] != "gpt-5" {
		t.Errorf("model top-level lost: %v", doc["model"])
	}
	mks, _ := doc["plugin_marketplaces"].(map[string]any)
	if _, ok := mks["existing"]; !ok {
		t.Error("existing marketplace not preserved")
	}
	if _, ok := mks["new"]; !ok {
		t.Error("new marketplace not added")
	}
}

func TestCodexRemoveMarketplaceDeletesEntry(t *testing.T) {
	body := `
[plugin_marketplaces.target]
source = "github:gone/repo"

[plugin_marketplaces.keep]
source = "github:keep/repo"
`
	home, path := setupHome(t, body)
	d := codex.New(stubRunner{})
	if err := d.RemoveMarketplace(provision.Context{HomeDir: home}, "target"); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	doc := map[string]any{}
	_ = toml.Unmarshal(raw, &doc)
	mks, _ := doc["plugin_marketplaces"].(map[string]any)
	if _, ok := mks["target"]; ok {
		t.Error("target marketplace still present after remove")
	}
	if _, ok := mks["keep"]; !ok {
		t.Error("unrelated marketplace was removed")
	}
}

func TestCodexRemoveMarketplaceMissingIsOK(t *testing.T) {
	home, _ := setupHome(t, "")
	d := codex.New(stubRunner{})
	if err := d.RemoveMarketplace(provision.Context{HomeDir: home}, "nonexistent"); err != nil {
		t.Errorf("removing absent marketplace should not error: %v", err)
	}
}

func TestCodexInstalledPluginsMissingFileEmpty(t *testing.T) {
	home := t.TempDir()
	if err := os.Mkdir(filepath.Join(home, ".codex"), 0o700); err != nil {
		t.Fatal(err)
	}
	d := codex.New(stubRunner{})
	got, err := d.InstalledPlugins(provision.Context{HomeDir: home})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty for missing file, got %+v", got)
	}
}

func TestCodexReadConfigMalformedReturnsError(t *testing.T) {
	home, path := setupHome(t, "not = valid = toml")
	_ = path
	d := codex.New(stubRunner{})
	if _, err := d.InstalledPlugins(provision.Context{HomeDir: home}); err == nil {
		t.Error("expected error for malformed TOML")
	}
}
