// Package pi provides the provision.Provisioner driver for Pi
// (`earendil-works/pi`, pi.dev). See
// docs/superpowers/specs/2026-08-31-opencode-pi-agent-support-design.md
// for the capabilities verified directly against the binary (nixpkgs
// package `pi-coding-agent`).
//
// Project trust: Pi only loads project-local config (.pi/settings.json,
// .pi/mcp.json) after a one-time interactive trust decision recorded in
// ~/.pi/agent/trust.json. aide never writes that file on the user's
// behalf; every user-scope setting this driver manages loads regardless.
package pi

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/jskswamy/aide/internal/provision"
	"github.com/jskswamy/aide/internal/provision/mcp"
)

const agentName = "pi"

// Driver implements provision.Provisioner for Pi. Capability stub
// methods are promoted from DriverBase.
type Driver struct {
	provision.DriverBase
	runner provision.Runner
}

// New returns a Driver using the supplied Runner. Pass
// provision.ExecRunner{} in production.
func New(r provision.Runner) *Driver {
	return &Driver{
		DriverBase: provision.DriverBase{Caps: provision.Capabilities{
			AgentName:           agentName,
			SupportsPlugins:     true,
			SupportsMCP:         true,
			RequiresTTY:         false,
			SourceShapes:        []provision.SourceShape{provision.ShapeURLDirect},
			PluginsByInstallRef: true,
			PluginNoun:          "packages",
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
// PI_CODING_AGENT_DIR is a documented env var (see `pi --help`'s
// environment variables section); its default is ~/.pi/agent.
func (d *Driver) AgentDir(ctx provision.Context) string {
	return agentDir(ctx)
}

func agentDir(ctx provision.Context) string {
	return provision.EnvOrHomePath(ctx, "PI_CODING_AGENT_DIR", ".pi", "agent")
}

// MCPConfigPath returns <AgentDir>/mcp.json, the user-level file Pi's
// built-in MCP client reads (pi 1.1.0+, docs/mcp.md).
func (*Driver) MCPConfigPath(ctx provision.Context) string {
	return filepath.Join(agentDir(ctx), "mcp.json")
}

// MCPHandler returns the JSON-flat handler: Pi's mcp.json uses the same
// top-level `mcpServers` map as Gemini and Copilot, and ignores the
// top-level _aide_managed marker (confirmed via `pi mcp list --json`).
func (*Driver) MCPHandler(_ provision.Context) provision.MCPHandler {
	return mcp.NewJSONFlat()
}

// piNotFoundStderr is appended to provision.DefaultTolerateStderr for
// UninstallPlugin. `pi remove` on a package that was never installed
// prints "No matching package found for X" (exit 1), which contains
// none of DefaultTolerateStderr's substrings.
const piNotFoundStderr = "No matching package found"

// InstallPlugin invokes `pi install <source> --no-approve`. The
// --no-approve flag keeps Pi from blocking on its project-trust prompt
// regardless of aide's working directory, which is what makes
// RequiresTTY: false hold.
func (d *Driver) InstallPlugin(ctx provision.Context, p provision.Plugin) error {
	return provision.RunCLI(context.Background(), d.runner, ctx,
		"pi install "+p.Name,
		"pi", []string{"install", p.Name, "--no-approve"})
}

// UninstallPlugin invokes `pi remove <source> --no-approve`, tolerating
// the standard rollback-safety stderr substrings plus Pi's own
// not-installed wording.
func (d *Driver) UninstallPlugin(ctx provision.Context, name string) error {
	tolerate := append(append([]string{}, provision.DefaultTolerateStderr...), piNotFoundStderr)
	return provision.RunCLI(context.Background(), d.runner, ctx,
		"pi remove "+name,
		"pi", []string{"remove", name, "--no-approve"},
		tolerate...)
}

// InstalledPlugins shells out to `pi list --no-approve` and parses its
// two-line-per-entry output:
//
//	User packages:
//	  <declared source>
//	    <resolved absolute path>
//
// provision.ParsePluginList assumes one entry per line, so this driver
// has its own parser. A Runner error (binary missing) collapses to
// (nil, nil): nothing is installed if Pi isn't.
func (d *Driver) InstalledPlugins(ctx provision.Context) ([]provision.Plugin, error) {
	stdout, stderr, code, err := d.runner.Run(context.Background(), ctx, "pi", "list", "--no-approve")
	if err != nil {
		return nil, nil
	}
	if code != 0 {
		return nil, fmt.Errorf("pi list: exit %d: %s", code, strings.TrimSpace(stderr))
	}
	return parsePackageList(stdout), nil
}

// parsePackageList extracts plugin entries from `pi list` output. Each
// entry is a declared-source line (two leading spaces) followed by its
// resolved absolute path (four leading spaces); section headers ("User
// packages:") and the empty-state message ("No packages installed.")
// have no indent and are skipped.
//
// Pi rewrites a local source relative to settings.json ("../../../x"),
// which matches neither the path declared in aide's config nor anything
// stable, so local packages are reported by their resolved absolute path
// instead (`pi remove` accepts it). Other sources (npm:, git:) are
// reported as declared.
func parsePackageList(out string) []provision.Plugin {
	var plugins []provision.Plugin
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "    "):
			last := len(plugins) - 1
			if last >= 0 && isLocalSource(plugins[last].Name) {
				resolved := strings.TrimSpace(line)
				plugins[last] = provision.Plugin{Key: resolved, Name: resolved}
			}
		case strings.HasPrefix(line, "  "):
			if source := strings.TrimSpace(line); source != "" {
				plugins = append(plugins, provision.Plugin{Key: source, Name: source})
			}
		}
	}
	return plugins
}

// isLocalSource reports whether a declared `pi list` source is a
// filesystem path rather than an npm:/git: reference.
func isLocalSource(source string) bool {
	return strings.HasPrefix(source, ".") || strings.HasPrefix(source, "/")
}

// The marketplace methods are inherited from DriverBase: Pi has no
// marketplace concept, only bare npm/git/local-path package sources.
