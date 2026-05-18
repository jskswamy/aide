// Package copilot provides the provision.Provisioner driver for the
// GitHub Copilot CLI (`copilot`, npm `@github/copilot`). See
// docs/specs/2026-05-16-agent-capability-research.md.
package copilot

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/jskswamy/aide/internal/provision"
	"github.com/jskswamy/aide/internal/provision/mcp"
)

const agentName = "copilot"

// Driver implements provision.Provisioner for GitHub Copilot CLI.
// The five capability stub methods are promoted from DriverBase.
type Driver struct {
	provision.DriverBase
	runner provision.Runner
}

// New returns a Driver using the supplied Runner.
func New(r provision.Runner) *Driver {
	return &Driver{
		DriverBase: provision.DriverBase{Caps: provision.Capabilities{
			AgentName:       agentName,
			SupportsPlugins: true,
			SupportsMCP:     true,
			RequiresTTY:     false,
			SourceShapes:    []provision.SourceShape{provision.ShapeMarketplace},
		}},
		runner: r,
	}
}

func init() {
	provision.RegisterProvisioner(New(provision.ExecRunner{}))
}

// MCPConfigPath returns ~/.copilot/mcp-config.json.
func (*Driver) MCPConfigPath(ctx provision.Context) string {
	return filepath.Join(ctx.HomeDir, ".copilot", "mcp-config.json")
}

// MCPHandler returns the JSON-flat handler (Copilot uses the same
// top-level `mcpServers` map as Gemini).
func (*Driver) MCPHandler(_ provision.Context) provision.MCPHandler {
	return mcp.NewJSONFlat()
}

// InstalledPlugins shells out to `copilot plugin list`. Output format
// is one plugin per line; we take the first token as the plugin name.
func (d *Driver) InstalledPlugins(pctx provision.Context) ([]provision.Plugin, error) {
	stdout, stderr, code, err := d.runner.Run(context.Background(), pctx.Env, "copilot", "plugin", "list")
	if err != nil {
		return nil, fmt.Errorf("copilot plugin list: %w", err)
	}
	if code != 0 {
		return nil, fmt.Errorf("copilot plugin list: exit %d: %s", code, stderr)
	}
	return parsePluginList(stdout), nil
}

func parsePluginList(out string) []provision.Plugin {
	var plugins []provision.Plugin
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "NAME") || strings.HasPrefix(line, "No plugins") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		name := fields[0]
		plugins = append(plugins, provision.Plugin{Key: name, Name: name})
	}
	return plugins
}

// InstallPlugin invokes `copilot plugin install <ref>`. For
// marketplace sources, ref is `<name>@<marketplace>`; for git/local
// sources, ref is the raw Plugin.Name value.
func (d *Driver) InstallPlugin(pctx provision.Context, p provision.Plugin) error {
	ref := p.Name
	return provision.RunCLI(context.Background(), d.runner, pctx.Env,
		"copilot plugin install "+ref,
		"copilot", []string{"plugin", "install", ref})
}

// UninstallPlugin invokes `copilot plugin uninstall <name>`. Tolerates
// the standard rollback-safety stderr substrings.
func (d *Driver) UninstallPlugin(pctx provision.Context, name string) error {
	return provision.RunCLI(context.Background(), d.runner, pctx.Env,
		"copilot plugin uninstall "+name,
		"copilot", []string{"plugin", "uninstall", name},
		provision.DefaultTolerateStderr...)
}

// InstalledMarketplaces returns the marketplaces registered with the
// Copilot CLI. Implementation is best-effort: the Copilot CLI surface
// for marketplaces is less stable than Claude's, so a binary-missing
// or non-zero exit collapses to an empty list rather than an error.
func (d *Driver) InstalledMarketplaces(pctx provision.Context) ([]provision.Marketplace, error) {
	stdout, stderr, code, err := d.runner.Run(context.Background(), pctx.Env, "copilot", "plugin", "marketplace", "list")
	if err != nil {
		return nil, nil
	}
	if code != 0 {
		// Marketplace surface may be unavailable; treat as empty rather
		// than fail the whole sync.
		_ = stderr
		return nil, nil
	}
	var out []provision.Marketplace
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "NAME") || strings.HasPrefix(line, "No marketplaces") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		out = append(out, provision.Marketplace{Name: fields[0], Source: fields[1], Key: fields[1]})
	}
	return out, nil
}

// AddMarketplace invokes `copilot plugin marketplace add <source>`.
func (d *Driver) AddMarketplace(pctx provision.Context, m provision.Marketplace) error {
	ref := m.Source
	if ref == "" {
		ref = m.Key
	}
	return provision.RunCLI(context.Background(), d.runner, pctx.Env,
		"copilot plugin marketplace add "+ref,
		"copilot", []string{"plugin", "marketplace", "add", ref})
}

// RemoveMarketplace invokes `copilot plugin marketplace remove <name>`.
// Tolerates the standard rollback-safety stderr substrings.
func (d *Driver) RemoveMarketplace(pctx provision.Context, name string) error {
	return provision.RunCLI(context.Background(), d.runner, pctx.Env,
		"copilot plugin marketplace remove "+name,
		"copilot", []string{"plugin", "marketplace", "remove", name},
		provision.DefaultTolerateStderr...)
}
