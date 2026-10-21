// Package opencode provides the provision.Provisioner driver for
// OpenCode (`anomalyco/opencode`, opencode.ai). See
// docs/superpowers/specs/2026-08-31-opencode-pi-agent-support-design.md
// for the capabilities verified directly against the binary.
package opencode

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/tailscale/hujson"

	"github.com/jskswamy/aide/internal/fsutil"
	"github.com/jskswamy/aide/internal/provision"
	"github.com/jskswamy/aide/internal/provision/mcp"
)

const agentName = "opencode"

// Driver implements provision.Provisioner for OpenCode. Capability
// stub methods are promoted from DriverBase. The Runner field is
// unused today (every operation is a file edit, not a shell-out) but
// kept for symmetry with other drivers and future use — the same
// choice the codex driver makes for the same reason.
type Driver struct {
	provision.DriverBase
	runner provision.Runner
}

// New returns a Driver using the supplied Runner.
func New(r provision.Runner) *Driver {
	return &Driver{
		DriverBase: provision.DriverBase{Caps: provision.Capabilities{
			AgentName:           agentName,
			SupportsPlugins:     true,
			SupportsMCP:         true,
			RequiresTTY:         false,
			SourceShapes:        []provision.SourceShape{provision.ShapeURLDirect},
			PluginsByInstallRef: true,
			SupportsHooks:       true,
			ProfileEnvKey:       provision.ProfileEnvKeyFor(agentName),
		}},
		runner: r,
	}
}

func init() {
	provision.RegisterProvisioner(New(provision.ExecRunner{}))
}

var _ provision.AgentDirProvider = (*Driver)(nil)

// AgentDir returns the absolute config directory for this context.
func (d *Driver) AgentDir(ctx provision.Context) string {
	return agentDir(ctx)
}

func agentDir(ctx provision.Context) string {
	return provision.EnvOrHomePath(ctx, "OPENCODE_CONFIG_DIR", ".config", "opencode")
}

// configPath returns the OpenCode config file path: OPENCODE_CONFIG
// if set, otherwise <agentDir>/opencode.jsonc.
func configPath(ctx provision.Context) string {
	if v := ctx.Env["OPENCODE_CONFIG"]; v != "" {
		return v
	}
	return filepath.Join(agentDir(ctx), "opencode.jsonc")
}

// MCPConfigPath returns the same file plugins live in — OpenCode
// keeps both under one config file.
func (*Driver) MCPConfigPath(ctx provision.Context) string { return configPath(ctx) }

// MCPHandler returns the file-edit handler — see
// internal/provision/mcp/opencodejson.go for why OpenCode's own CLI
// isn't usable here.
func (*Driver) MCPHandler(_ provision.Context) provision.MCPHandler { return mcp.NewOpenCodeJSON() }

// readConfig reads and JSONC-normalizes opencode.jsonc into a generic
// map, the same read-whole-doc-mutate-one-key-write-whole-doc pattern
// codex's config.toml handling uses, so plugins and MCP (Task 2, a
// different key in the same file) never clobber each other's keys.
func readConfig(path string) (map[string]any, error) {
	return provision.ReadGenericDoc(agentName, path, func(data []byte) (map[string]any, error) {
		standardized, err := hujson.Standardize(data)
		if err != nil {
			return nil, err
		}
		doc := map[string]any{}
		err = json.Unmarshal(standardized, &doc)
		return doc, err
	})
}

func writeConfig(path string, doc map[string]any) error {
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("opencode: marshalling config %s: %w", path, err)
	}
	return fsutil.AtomicWrite(path, out)
}

// pluginRefs reads the "plugin" array (bare npm/git/local-path
// strings — confirmed via `opencode debug config`: locally-dropped
// hook plugins from .opencode/plugin/ show up here too, as file://
// URIs, alongside any explicitly-declared refs; they coexist without
// collision since aide only ever adds/removes plain string refs it
// itself declared).
func pluginRefs(doc map[string]any) []string {
	raw, ok := doc["plugin"].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func setPluginRefs(doc map[string]any, refs []string) {
	list := make([]any, len(refs))
	for i, r := range refs {
		list[i] = r
	}
	doc["plugin"] = list
}

// InstalledPlugins reads the "plugin" array directly. Binary-missing
// isn't a concept here (no shell-out), so a missing config file is
// simply "nothing installed", matching the InstalledPlugins
// convention of every other driver.
func (d *Driver) InstalledPlugins(ctx provision.Context) ([]provision.Plugin, error) {
	doc, err := readConfig(configPath(ctx))
	if err != nil {
		return nil, err
	}
	refs := pluginRefs(doc)
	out := make([]provision.Plugin, 0, len(refs))
	for _, ref := range refs {
		out = append(out, provision.Plugin{Key: ref, Name: ref})
	}
	return out, nil
}

// InstallPlugin appends p.Name to the "plugin" array if not already
// present. No CLI is shelled out to — see the package doc comment on
// opencodejson.go for why: `opencode plugin <module>` only accepts
// npm module names (confirmed via --help), can't express git/local
// refs, and has no list/remove counterpart at all.
func (d *Driver) InstallPlugin(ctx provision.Context, p provision.Plugin) error {
	path := configPath(ctx)
	doc, err := readConfig(path)
	if err != nil {
		return err
	}
	refs := pluginRefs(doc)
	for _, existing := range refs {
		if existing == p.Name {
			return nil
		}
	}
	setPluginRefs(doc, append(refs, p.Name))
	return writeConfig(path, doc)
}

// UninstallPlugin removes name from the "plugin" array. No-op (nil
// error) if not present, for rollback safety.
func (d *Driver) UninstallPlugin(ctx provision.Context, name string) error {
	path := configPath(ctx)
	doc, err := readConfig(path)
	if err != nil {
		return err
	}
	refs := pluginRefs(doc)
	kept := make([]string, 0, len(refs))
	for _, existing := range refs {
		if existing != name {
			kept = append(kept, existing)
		}
	}
	if len(kept) == len(refs) {
		return nil
	}
	setPluginRefs(doc, kept)
	return writeConfig(path, doc)
}

// The marketplace methods are inherited from DriverBase: OpenCode has
// no marketplace concept, only bare npm/git/local-path refs.
