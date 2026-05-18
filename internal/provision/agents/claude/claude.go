// Package claude provides the provision.Provisioner driver for
// Anthropic's Claude Code CLI (`claude`). See
// docs/specs/2026-05-16-agent-capability-research.md.
//
// Notes:
//   - Plugin install/uninstall/list all have non-interactive CLI
//     surfaces: `claude plugin install <ref>`, `claude plugin
//     uninstall <ref>`, and `claude plugin list --json`. The research
//     doc's "TUI-only" assessment was stale — verified against
//     `claude plugin --help` 2026-05-17.
package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jskswamy/aide/internal/provision"
	"github.com/jskswamy/aide/internal/provision/mcp"
)

const agentName = "claude"

// Driver implements provision.Provisioner for Claude Code. The
// capability stub methods (Name, SupportsPlugins, SupportsMCP,
// RequiresTTY, SupportedSourceShapes) are promoted from the
// embedded DriverBase; Claude's plugin CLI surface
// (install/uninstall/list --json) is fully non-interactive, so
// RequiresTTY stays false.
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
			ProfileEnvKey:   "CLAUDE_CONFIG_DIR",
		}},
		runner: r,
	}
}

func init() {
	provision.RegisterProvisioner(New(provision.ExecRunner{}))
}

// MCPConfigPath returns the project-scope `.mcp.json` at the context's
// project root. Project scope is the default for aide so contexts
// stay self-contained; user-scope MCP at ~/.claude.json can be added
// later by switching the handler shape.
func (*Driver) MCPConfigPath(ctx provision.Context) string {
	root := projectRoot(ctx)
	if root == "" {
		// Fallback: write under HomeDir to avoid blowing up.
		return filepath.Join(ctx.HomeDir, ".mcp.json")
	}
	return filepath.Join(root, ".mcp.json")
}

// MCPHandler returns the Claude JSON handler. Project root comes from
// the resolved context; the handler will autodetect flat vs nested
// shape.
func (*Driver) MCPHandler(ctx provision.Context) provision.MCPHandler {
	return mcp.NewClaudeJSON(projectRoot(ctx))
}

// projectRoot returns ctx.ProjectRoot, falling back to os.Getwd() so
// older callers (and tests that build a bare Context) still work.
func projectRoot(ctx provision.Context) string {
	if ctx.ProjectRoot != "" {
		return ctx.ProjectRoot
	}
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return wd
}

// claudePluginEntry mirrors the shape of one element from
// `claude plugin list --json`. The `id` field is `<name>@<marketplace>`.
type claudePluginEntry struct {
	ID      string `json:"id"`
	Version string `json:"version,omitempty"`
	Enabled bool   `json:"enabled,omitempty"`
}

// InstalledPlugins invokes `claude plugin list --json` and converts
// each entry into a provision.Plugin. The id field (`<name>@<marketplace>`)
// becomes Plugin.Name; the leading `<name>` part becomes Plugin.Key so
// the engine's diff aligns with how aide.yaml declares plugins.
//
// If the `claude` binary is missing (`exec.LookPath` failure surfaced
// by the Runner), returns an empty slice and a nil error — treating
// "agent not installed" as "nothing installed" lets `aide plugin list`
// still render a useful declared/managed view.
func (d *Driver) InstalledPlugins(pctx provision.Context) ([]provision.Plugin, error) {
	stdout, stderr, code, err := d.runner.Run(context.Background(), pctx.Env, "claude", "plugin", "list", "--json")
	if err != nil {
		// Binary missing or unrunnable — return empty rather than fail.
		return nil, nil
	}
	if code != 0 {
		return nil, fmt.Errorf("claude plugin list --json: exit %d: %s", code, strings.TrimSpace(stderr))
	}
	var entries []claudePluginEntry
	if err := json.Unmarshal([]byte(stdout), &entries); err != nil {
		return nil, fmt.Errorf("parsing claude plugin list --json: %w", err)
	}
	out := make([]provision.Plugin, 0, len(entries))
	for _, e := range entries {
		key := e.ID
		if at := strings.IndexByte(e.ID, '@'); at > 0 {
			key = e.ID[:at]
		}
		out = append(out, provision.Plugin{
			Key:    key,
			Source: "marketplace",
			Name:   e.ID,
		})
	}
	return out, nil
}

// InstallPlugin invokes `claude plugin install <ref>`. The ref shape
// is `<name>@<marketplace>` per Claude docs.
func (d *Driver) InstallPlugin(pctx provision.Context, p provision.Plugin) error {
	return provision.RunCLI(context.Background(), d.runner, pctx.Env,
		"claude plugin install "+p.Name,
		"claude", []string{"plugin", "install", p.Name})
}

// UninstallPlugin invokes `claude plugin uninstall <ref>`. Tolerates
// "not installed" / "not found" stderr for rollback safety.
func (d *Driver) UninstallPlugin(pctx provision.Context, name string) error {
	return provision.RunCLI(context.Background(), d.runner, pctx.Env,
		"claude plugin uninstall "+name,
		"claude", []string{"plugin", "uninstall", name},
		provision.DefaultTolerateStderr...)
}

// claudeMarketplaceEntry mirrors the shape of one element from
// `claude plugin marketplace list --json`. Verified 2026-05-18:
//
//	{
//	  "name": "beads-marketplace",      // canonical marketplace name
//	  "source": "github",                // transport type
//	  "repo": "steveyegge/beads",        // repo path (path/url depending on source)
//	  "installLocation": "..."
//	}
//
// The earlier shape assumed `source` carried the prefixed repo path
// (e.g. "github:steveyegge/beads"); that was wrong against the real
// CLI output.
type claudeMarketplaceEntry struct {
	Name   string `json:"name"`
	Source string `json:"source"`
	Repo   string `json:"repo"`
}

// InstalledMarketplaces invokes `claude plugin marketplace list --json`
// and returns the configured marketplaces. Binary-missing surfaces as
// (nil, nil) so callers can still render a useful declared-only view.
//
// The returned Marketplace.Key is the bare repo path (e.g.
// "steveyegge/beads") so it matches the desired-side keys derived from
// raw YAML map keys in the user's config. Marketplace.Source carries
// the prefixed form ("github:steveyegge/beads") for parity with
// provision.ResolveDesired's keyAsSource output.
func (d *Driver) InstalledMarketplaces(pctx provision.Context) ([]provision.Marketplace, error) {
	stdout, stderr, code, err := d.runner.Run(context.Background(), pctx.Env, "claude", "plugin", "marketplace", "list", "--json")
	if err != nil {
		return nil, nil
	}
	if code != 0 {
		return nil, fmt.Errorf("claude plugin marketplace list --json: exit %d: %s", code, strings.TrimSpace(stderr))
	}
	var entries []claudeMarketplaceEntry
	if err := json.Unmarshal([]byte(stdout), &entries); err != nil {
		return nil, fmt.Errorf("parsing claude marketplace list: %w", err)
	}
	out := make([]provision.Marketplace, 0, len(entries))
	for _, e := range entries {
		// Compose the source ref as <transport>:<repo>. For pure-name
		// sources (no repo), fall back to using the name as the key.
		key := e.Repo
		source := e.Source + ":" + e.Repo
		if key == "" {
			key = e.Name
			source = e.Source
		}
		out = append(out, provision.Marketplace{
			Key:    key,
			Source: source,
			Name:   e.Name,
		})
	}
	return out, nil
}

// AddMarketplace invokes `claude plugin marketplace add <source>`.
func (d *Driver) AddMarketplace(pctx provision.Context, m provision.Marketplace) error {
	ref := normalizeMarketplaceRef(m.Source, m.Key)
	return provision.RunCLI(context.Background(), d.runner, pctx.Env,
		"claude plugin marketplace add "+ref,
		"claude", []string{"plugin", "marketplace", "add", ref})
}

// RemoveMarketplace invokes `claude plugin marketplace remove <name>`.
// Tolerates the standard "not found" / "not configured" / "not
// installed" stderr substrings for rollback safety.
func (d *Driver) RemoveMarketplace(pctx provision.Context, name string) error {
	return provision.RunCLI(context.Background(), d.runner, pctx.Env,
		"claude plugin marketplace remove "+name,
		"claude", []string{"plugin", "marketplace", "remove", name},
		provision.DefaultTolerateStderr...)
}

// normalizeMarketplaceRef converts aide's internal source representation
// (`github:owner/repo`) to the form claude's `plugin marketplace add`
// accepts (bare `owner/repo`). Full URLs and local paths pass through
// unchanged. The `github:` prefix is an aide-internal normalization to
// keep parity with how `marketplace list --json` emits its source field
// as the type + repo concatenation — claude itself rejects that prefix
// on the add side ("Invalid marketplace source format. Try:
// owner/repo, https://..., or ./path"). Delegates to
// provision.ParseSourceRef so the prefix vocabulary lives in one place.
func normalizeMarketplaceRef(source, key string) string {
	ref := source
	if ref == "" {
		ref = key
	}
	return provision.ParseSourceRef(ref).Bare()
}
