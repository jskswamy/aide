// Package codex provides the provision.Provisioner driver for
// OpenAI's Codex CLI (`codex`). See
// docs/specs/2026-05-16-agent-capability-research.md.
//
// Codex's plugin install flow is TUI-leaning, but per research the
// `npx codex-marketplace` helper writes plugin metadata into
// `~/.codex/plugins/cache` and toggles `[plugins."<name>@<source>"]
// enabled = true` in `config.toml`. This driver treats plugin
// install/uninstall as a TOML-edit on `config.toml` so it works
// without a TUI. First-time installs from a fresh marketplace may
// still require running the marketplace TUI to seed the cache; that
// limitation is documented but out of scope for this driver.
package codex

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/pelletier/go-toml/v2"

	"github.com/jskswamy/aide/internal/fsutil"
	"github.com/jskswamy/aide/internal/provision"
	"github.com/jskswamy/aide/internal/provision/mcp"
)

const agentName = "codex"

// Driver implements provision.Provisioner for Codex CLI.
type Driver struct {
	runner provision.Runner
}

// New returns a Driver. Codex's mutations are file-edits so the
// Runner is currently unused; we accept it for symmetry with the
// other drivers and future use.
func New(r provision.Runner) *Driver { return &Driver{runner: r} }

func init() {
	provision.RegisterProvisioner(New(provision.ExecRunner{}))
}

// Name implements provision.Provisioner.
func (*Driver) Name() string { return agentName }

// SupportsPlugins implements provision.Provisioner.
func (*Driver) SupportsPlugins() bool { return true }

// SupportsMCP implements provision.Provisioner.
func (*Driver) SupportsMCP() bool { return true }

// RequiresTTY implements provision.Provisioner. We toggle TOML
// directly, so no terminal is needed.
func (*Driver) RequiresTTY() bool { return false }

// SupportedSourceShapes implements provision.Provisioner.
func (*Driver) SupportedSourceShapes() []provision.SourceShape {
	return []provision.SourceShape{provision.ShapeMarketplace}
}

// MCPConfigPath returns ~/.codex/config.toml.
func (*Driver) MCPConfigPath(ctx provision.Context) string {
	return filepath.Join(ctx.HomeDir, ".codex", "config.toml")
}

// MCPHandler returns the Codex-TOML handler.
func (*Driver) MCPHandler(_ provision.Context) provision.MCPHandler {
	return mcp.NewCodexTOML()
}

// configPath is the same file as MCPConfigPath; plugins live there too.
func (d *Driver) configPath(ctx provision.Context) string { return d.MCPConfigPath(ctx) }

// InstalledPlugins reads config.toml and returns plugins whose
// `enabled` field is true. Plugin tables live at
// [plugins."<name>@<source>"].
func (d *Driver) InstalledPlugins(ctx provision.Context) ([]provision.Plugin, error) {
	doc, err := readConfig(d.configPath(ctx))
	if err != nil {
		return nil, err
	}
	plugins := []provision.Plugin{}
	if raw, ok := doc["plugins"]; ok {
		if m, ok := raw.(map[string]any); ok {
			keys := make([]string, 0, len(m))
			for k := range m {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, key := range keys {
				body, ok := m[key].(map[string]any)
				if !ok {
					continue
				}
				if enabled, _ := body["enabled"].(bool); enabled {
					plugins = append(plugins, provision.Plugin{Key: key, Name: key})
				}
			}
		}
	}
	return plugins, nil
}

// InstallPlugin toggles `[plugins."<ref>"] enabled = true` in
// config.toml.
func (d *Driver) InstallPlugin(ctx provision.Context, p provision.Plugin) error {
	return d.setPluginEnabled(ctx, p.Name, true)
}

// UninstallPlugin sets `enabled = false` for the plugin. We don't
// delete the table because the plugin's cached metadata may still be
// needed; disabling is the documented inverse of marketplace install.
func (d *Driver) UninstallPlugin(ctx provision.Context, name string) error {
	return d.setPluginEnabled(ctx, name, false)
}

func (d *Driver) setPluginEnabled(ctx provision.Context, name string, enabled bool) error {
	path := d.configPath(ctx)
	doc, err := readConfig(path)
	if err != nil {
		return err
	}
	plugins := map[string]any{}
	if raw, ok := doc["plugins"]; ok {
		if m, ok := raw.(map[string]any); ok {
			plugins = m
		}
	}
	body, _ := plugins[name].(map[string]any)
	if body == nil {
		body = map[string]any{}
	}
	body["enabled"] = enabled
	plugins[name] = body
	doc["plugins"] = plugins

	out, err := toml.Marshal(doc)
	if err != nil {
		return fmt.Errorf("codex: marshalling config: %w", err)
	}
	return fsutil.AtomicWrite(path, out)
}

func readConfig(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]any{}, nil
		}
		return nil, fmt.Errorf("codex: reading config %s: %w", path, err)
	}
	doc := map[string]any{}
	if err := toml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("codex: parsing config %s: %w", path, err)
	}
	return doc, nil
}

// InstalledMarketplaces reads `[plugin_marketplaces.<name>]` tables
// from config.toml and returns their entries.
func (d *Driver) InstalledMarketplaces(ctx provision.Context) ([]provision.Marketplace, error) {
	doc, err := readConfig(d.configPath(ctx))
	if err != nil {
		return nil, err
	}
	var out []provision.Marketplace
	if raw, ok := doc["plugin_marketplaces"]; ok {
		if m, ok := raw.(map[string]any); ok {
			names := make([]string, 0, len(m))
			for k := range m {
				names = append(names, k)
			}
			sort.Strings(names)
			for _, n := range names {
				body, _ := m[n].(map[string]any)
				src, _ := body["source"].(string)
				out = append(out, provision.Marketplace{Name: n, Source: src, Key: src})
			}
		}
	}
	return out, nil
}

// AddMarketplace writes `[plugin_marketplaces.<key>]` with the given
// source ref into config.toml. The name is derived from the key for now.
func (d *Driver) AddMarketplace(ctx provision.Context, m provision.Marketplace) error {
	path := d.configPath(ctx)
	doc, err := readConfig(path)
	if err != nil {
		return err
	}
	mks := map[string]any{}
	if raw, ok := doc["plugin_marketplaces"]; ok {
		if mm, ok := raw.(map[string]any); ok {
			mks = mm
		}
	}
	name := m.Name
	if name == "" {
		name = m.Key
	}
	body, _ := mks[name].(map[string]any)
	if body == nil {
		body = map[string]any{}
	}
	src := m.Source
	if src == "" {
		src = m.Key
	}
	body["source"] = src
	mks[name] = body
	doc["plugin_marketplaces"] = mks
	out, err := toml.Marshal(doc)
	if err != nil {
		return fmt.Errorf("codex: marshalling config: %w", err)
	}
	return fsutil.AtomicWrite(path, out)
}

// RemoveMarketplace deletes the `[plugin_marketplaces.<name>]` table
// from config.toml. Tolerates missing entries.
func (d *Driver) RemoveMarketplace(ctx provision.Context, name string) error {
	path := d.configPath(ctx)
	doc, err := readConfig(path)
	if err != nil {
		return err
	}
	if raw, ok := doc["plugin_marketplaces"]; ok {
		if mm, ok := raw.(map[string]any); ok {
			delete(mm, name)
			doc["plugin_marketplaces"] = mm
		}
	}
	out, err := toml.Marshal(doc)
	if err != nil {
		return fmt.Errorf("codex: marshalling config: %w", err)
	}
	return fsutil.AtomicWrite(path, out)
}
