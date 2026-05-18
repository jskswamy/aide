// Package gemini provides the provision.Provisioner driver for Google
// Gemini CLI (`gemini`). Plugins are called "extensions" in Gemini's
// terminology. See docs/specs/2026-05-16-agent-capability-research.md
// for the verified CLI surface.
package gemini

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/jskswamy/aide/internal/provision"
	"github.com/jskswamy/aide/internal/provision/mcp"
)

const agentName = "gemini"

// Driver implements provision.Provisioner for Gemini CLI. Capability
// stub methods are promoted from DriverBase.
type Driver struct {
	provision.DriverBase
	runner provision.Runner
}

// New returns a Driver using the supplied Runner. Pass
// provision.ExecRunner{} in production.
func New(r provision.Runner) *Driver {
	return &Driver{
		DriverBase: provision.DriverBase{Caps: provision.Capabilities{
			AgentName:       agentName,
			SupportsPlugins: true,
			SupportsMCP:     true,
			RequiresTTY:     false,
			SourceShapes:    []provision.SourceShape{provision.ShapeURLDirect},
		}},
		runner: r,
	}
}

func init() {
	provision.RegisterProvisioner(New(provision.ExecRunner{}))
}

// MCPConfigPath returns ~/.gemini/settings.json.
func (*Driver) MCPConfigPath(ctx provision.Context) string {
	return filepath.Join(ctx.HomeDir, ".gemini", "settings.json")
}

// MCPHandler returns the JSON-flat handler (Gemini stores MCP at the
// top-level `mcpServers` key).
func (*Driver) MCPHandler(_ provision.Context) provision.MCPHandler {
	return mcp.NewJSONFlat()
}

// InstalledPlugins shells out to `gemini extensions list` and parses
// its output. The list output is one extension per line, with the
// name appearing as the first whitespace-delimited token. Empty lines
// and obvious headers are skipped.
func (d *Driver) InstalledPlugins(pctx provision.Context) ([]provision.Plugin, error) {
	stdout, stderr, code, err := d.runner.Run(context.Background(), pctx.Env, "gemini", "extensions", "list")
	if err != nil {
		return nil, fmt.Errorf("gemini extensions list: %w", err)
	}
	if code != 0 {
		return nil, fmt.Errorf("gemini extensions list: exit %d: %s", code, stderr)
	}
	return parseExtensionsList(stdout), nil
}

func parseExtensionsList(out string) []provision.Plugin {
	var plugins []provision.Plugin
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Skip lines that look like headers ("NAME ...") or messages.
		if strings.HasPrefix(line, "NAME") || strings.HasPrefix(line, "No extensions") {
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

// InstallPlugin invokes `gemini extensions install <ref>`. The ref is
// derived from the plugin source:
//   - marketplace/git: use Plugin.Name (a GitHub URL or owner/repo).
//   - local: use --path Plugin.Name (an absolute or relative path).
func (d *Driver) InstallPlugin(pctx provision.Context, p provision.Plugin) error {
	args := installArgs(p)
	_, stderr, code, err := d.runner.Run(context.Background(), pctx.Env, "gemini", args...)
	if err != nil {
		return fmt.Errorf("gemini extensions install %s: %w", p.Name, err)
	}
	if code != 0 {
		return fmt.Errorf("gemini extensions install %s: exit %d: %s", p.Name, code, stderr)
	}
	return nil
}

func installArgs(p provision.Plugin) []string {
	switch p.Source {
	case "local":
		return []string{"extensions", "install", "--path", p.Name}
	default:
		// marketplace, git, http: Gemini accepts a single positional ref.
		return []string{"extensions", "install", p.Name}
	}
}

// UninstallPlugin invokes `gemini extensions uninstall <name>`.
func (d *Driver) UninstallPlugin(pctx provision.Context, name string) error {
	_, stderr, code, err := d.runner.Run(context.Background(), pctx.Env, "gemini", "extensions", "uninstall", name)
	if err != nil {
		return fmt.Errorf("gemini extensions uninstall %s: %w", name, err)
	}
	if code != 0 {
		// Treat missing-extension as success so rollback is safe.
		if strings.Contains(stderr, "not installed") || strings.Contains(stderr, "not found") {
			return nil
		}
		return fmt.Errorf("gemini extensions uninstall %s: exit %d: %s", name, code, stderr)
	}
	return nil
}

// InstalledMarketplaces is a no-op: Gemini does not have a marketplace
// concept. Returns nil, nil.
func (*Driver) InstalledMarketplaces(_ provision.Context) ([]provision.Marketplace, error) {
	return nil, nil
}

// AddMarketplace returns an error: Gemini extensions are declared as
// URL-direct string entries, not via marketplaces.
func (*Driver) AddMarketplace(_ provision.Context, _ provision.Marketplace) error {
	return fmt.Errorf("gemini does not have marketplaces; declare extensions inline with string values")
}

// RemoveMarketplace is a no-op for rollback safety.
func (*Driver) RemoveMarketplace(_ provision.Context, _ string) error {
	return nil
}
