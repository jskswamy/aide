// Package omp provides the provision.Provisioner driver for Oh My Pi
// (`can1357/oh-my-pi`, omp).
package omp

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"path/filepath"
	"strings"

	"github.com/jskswamy/aide/internal/provision"
	"github.com/jskswamy/aide/internal/provision/mcp"
)

const agentName = "omp"

// Driver implements provision.Provisioner for Oh My Pi (omp). Capability
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
			SupportsHooks:   true,
			ProfileEnvKey:   provision.ProfileEnvKeyFor(agentName),
		}},
		runner: r,
	}
}

func init() {
	provision.RegisterProvisioner(New(provision.ExecRunner{}))
}

var _ provision.AgentDirProvider = (*Driver)(nil)

// AgentDir returns the absolute config directory for this context.
// PI_CODING_AGENT_DIR relocates OMP's agent directory (default ~/.omp/agent).
func (d *Driver) AgentDir(ctx provision.Context) string {
	return agentDir(ctx)
}

func agentDir(ctx provision.Context) string {
	return provision.EnvOrHomePath(ctx, "PI_CODING_AGENT_DIR", ".omp", "agent")
}

// withConfigRoot mirrors PI_CODING_AGENT_DIR into a home-relative
// PI_CONFIG_DIR when an isolated profile dir under HomeDir is active, so
// `omp install` and `omp plugin` store plugins under <profile-dir>/plugins
// rather than sharing ~/.omp/plugins across profiles.
func withConfigRoot(ctx provision.Context) provision.Context {
	dir := ctx.Env["PI_CODING_AGENT_DIR"]
	if dir == "" || ctx.HomeDir == "" || ctx.Env["PI_CONFIG_DIR"] != "" || !filepath.IsAbs(dir) {
		return ctx
	}
	cleanHome := filepath.Clean(ctx.HomeDir)
	cleanDir := filepath.Clean(dir)
	if cleanDir == filepath.Join(cleanHome, ".omp", "agent") {
		return ctx
	}
	rel, err := filepath.Rel(cleanHome, cleanDir)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ctx
	}
	env := maps.Clone(ctx.Env)
	env["PI_CONFIG_DIR"] = filepath.ToSlash(rel)
	ctx.Env = env
	return ctx
}

// MCPConfigPath returns <AgentDir>/mcp.json, OMP's native user-level MCP file.
func (*Driver) MCPConfigPath(ctx provision.Context) string {
	return filepath.Join(agentDir(ctx), "mcp.json")
}

// MCPHandler returns the JSON-flat handler: OMP's mcp.json uses the same
// top-level `mcpServers` map as Pi, Gemini and Copilot.
func (*Driver) MCPHandler(_ provision.Context) provision.MCPHandler {
	return mcp.NewJSONFlat()
}

// InstallPlugin invokes `omp install <target>`, which links local paths
// and installs npm or git specs into the active plugins root.
func (d *Driver) InstallPlugin(ctx provision.Context, p provision.Plugin) error {
	target := strings.TrimPrefix(p.Name, "npm:")
	return provision.RunCLI(context.Background(), d.runner, withConfigRoot(ctx),
		"omp install "+target,
		"omp", []string{"install", target})
}

// UninstallPlugin invokes `omp plugin uninstall <name>`, tolerating the
// standard rollback-safety stderr substrings ("not installed").
func (d *Driver) UninstallPlugin(ctx provision.Context, name string) error {
	return provision.RunCLI(context.Background(), d.runner, withConfigRoot(ctx),
		"omp plugin uninstall "+name,
		"omp", []string{"plugin", "uninstall", name},
		provision.DefaultTolerateStderr...)
}

type ompPluginListOutput struct {
	NPM []struct {
		Name string `json:"name"`
	} `json:"npm"`
	Marketplace []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"marketplace"`
}

// InstalledPlugins invokes `omp plugin list --json` and returns installed
// plugin entries keyed by package name. A Runner error (binary missing)
// collapses to (nil, nil), matching the claude and pi drivers.
func (d *Driver) InstalledPlugins(ctx provision.Context) ([]provision.Plugin, error) {
	stdout, stderr, code, err := d.runner.Run(context.Background(), withConfigRoot(ctx), "omp", "plugin", "list", "--json")
	if err != nil {
		return nil, nil
	}
	if code != 0 {
		return nil, fmt.Errorf("omp plugin list --json: exit %d: %s", code, strings.TrimSpace(stderr))
	}
	var parsed ompPluginListOutput
	if err := json.Unmarshal([]byte(stdout), &parsed); err != nil {
		return nil, fmt.Errorf("parsing omp plugin list --json: %w", err)
	}
	var out []provision.Plugin
	for _, e := range parsed.NPM {
		if e.Name != "" {
			out = append(out, provision.Plugin{Key: e.Name, Name: e.Name})
		}
	}
	for _, m := range parsed.Marketplace {
		id := m.ID
		if id == "" {
			id = m.Name
		}
		if id == "" {
			continue
		}
		key := id
		if at := strings.IndexByte(id, '@'); at > 0 {
			key = id[:at]
		}
		out = append(out, provision.Plugin{Key: key, Source: "marketplace", Name: id})
	}
	return out, nil
}
