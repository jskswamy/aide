package claude_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/jskswamy/aide/internal/provision"
	"github.com/jskswamy/aide/internal/provision/agents/claude"
)

type fakeRunner struct {
	stdout string
	stderr string
	code   int
	err    error
	calls  [][]string
}

func (f *fakeRunner) Run(_ context.Context, env map[string]string, name string, args ...string) (string, string, int, error) {
	_ = env
	call := append([]string{name}, args...)
	f.calls = append(f.calls, call)
	return f.stdout, f.stderr, f.code, f.err
}

func TestClaudeCapabilities(t *testing.T) {
	d := claude.New(&fakeRunner{})
	if d.Name() != "claude" {
		t.Errorf("Name = %q", d.Name())
	}
	if !d.SupportsPlugins() || !d.SupportsMCP() {
		t.Error("Claude should support plugins and MCP")
	}
	if d.RequiresTTY() {
		t.Error("Claude plugin CLI is non-interactive; RequiresTTY should be false")
	}
	shapes := d.SupportedSourceShapes()
	if len(shapes) != 1 || shapes[0] != provision.ShapeMarketplace {
		t.Errorf("Claude shapes = %v, want [marketplace]", shapes)
	}
}

func TestClaudeInstallPluginCommand(t *testing.T) {
	r := &fakeRunner{}
	d := claude.New(r)
	err := d.InstallPlugin(provision.Context{}, provision.Plugin{Key: "linear", Source: "marketplace", Name: "linear@anthropic"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"claude", "plugin", "install", "linear@anthropic"}
	if !reflect.DeepEqual(r.calls[0], want) {
		t.Errorf("call = %v, want %v", r.calls[0], want)
	}
}

func TestClaudeUninstallPluginCommand(t *testing.T) {
	r := &fakeRunner{}
	d := claude.New(r)
	if err := d.UninstallPlugin(provision.Context{}, "linear@anthropic"); err != nil {
		t.Fatal(err)
	}
	want := []string{"claude", "plugin", "uninstall", "linear@anthropic"}
	if !reflect.DeepEqual(r.calls[0], want) {
		t.Errorf("call = %v", r.calls[0])
	}
}

func TestClaudeUninstallMissingTolerated(t *testing.T) {
	r := &fakeRunner{code: 1, stderr: "plugin 'x' not installed"}
	d := claude.New(r)
	if err := d.UninstallPlugin(provision.Context{}, "x"); err != nil {
		t.Errorf("missing plugin should be tolerated: %v", err)
	}
}

// TestClaudeMCPConfigPathEmpty pins the contract that claude is
// CLI-driven for MCP: the file path is intentionally unset so any
// regression that tries to fall back to file-edit code fails loudly.
func TestClaudeMCPConfigPathEmpty(t *testing.T) {
	d := claude.New(&fakeRunner{})
	if got := d.MCPConfigPath(provision.Context{ProjectRoot: "/tmp/proj", HomeDir: "/home/u"}); got != "" {
		t.Errorf("MCPConfigPath should be empty for CLI-driven driver, got %q", got)
	}
	if h := d.MCPHandler(provision.Context{HomeDir: "/home/u"}); h != nil {
		t.Errorf("MCPHandler should be nil for CLI-driven driver, got %T", h)
	}
}

func TestClaudeInstalledPluginsParsesJSON(t *testing.T) {
	r := &fakeRunner{stdout: `[
		{"id":"beads@beads-marketplace","version":"1.0.4","enabled":true},
		{"id":"craft@jskswamy-plugins","version":"1.0.0","enabled":true}
	]`}
	d := claude.New(r)
	got, err := d.InstalledPlugins(provision.Context{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 plugins, got %d", len(got))
	}
	if got[0].Key != "beads" || got[0].Name != "beads@beads-marketplace" || got[0].Source != "marketplace" {
		t.Errorf("plugin[0] = %+v", got[0])
	}
	if got[1].Key != "craft" || got[1].Name != "craft@jskswamy-plugins" {
		t.Errorf("plugin[1] = %+v", got[1])
	}
	want := []string{"claude", "plugin", "list", "--json"}
	if !reflect.DeepEqual(r.calls[0], want) {
		t.Errorf("call = %v, want %v", r.calls[0], want)
	}
}

func TestClaudeInstalledMarketplaces(t *testing.T) {
	// Real shape from `claude plugin marketplace list --json` (verified
	// 2026-05-18): name is the canonical marketplace name, source is the
	// transport type (e.g. "github"), and repo carries the actual path.
	// The driver must build Marketplace.Key from repo so it matches
	// desired-side keys derived from user YAML (raw owner/repo strings).
	r := &fakeRunner{stdout: `[
		{"name":"beads-marketplace","source":"github","repo":"steveyegge/beads","installLocation":"/x/beads"},
		{"name":"jskswamy-plugins","source":"github","repo":"jskswamy/claude-plugins","installLocation":"/x/jp"}
	]`}
	d := claude.New(r)
	got, err := d.InstalledMarketplaces(provision.Context{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d marketplaces, want 2", len(got))
	}
	// Key must equal the raw repo path so it matches desired-side YAML keys.
	if got[0].Key != "steveyegge/beads" {
		t.Errorf("got[0].Key = %q, want %q", got[0].Key, "steveyegge/beads")
	}
	if got[0].Name != "beads-marketplace" {
		t.Errorf("got[0].Name = %q, want beads-marketplace", got[0].Name)
	}
	if got[0].Source != "github:steveyegge/beads" {
		t.Errorf("got[0].Source = %q, want github:steveyegge/beads", got[0].Source)
	}
	want := []string{"claude", "plugin", "marketplace", "list", "--json"}
	if !reflect.DeepEqual(r.calls[0], want) {
		t.Errorf("call = %v", r.calls[0])
	}
}

func TestClaudeAddMarketplace(t *testing.T) {
	// Claude's `plugin marketplace add` accepts bare owner/repo or a
	// full URL; it rejects the `github:` shorthand we use internally
	// to normalize sources (verified 2026-05-18: "Invalid marketplace
	// source format. Try: owner/repo, https://..., or ./path"). The
	// driver must strip the `github:` prefix before invoking claude.
	r := &fakeRunner{}
	d := claude.New(r)
	err := d.AddMarketplace(provision.Context{}, provision.Marketplace{Key: "steveyegge/beads", Source: "github:steveyegge/beads"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"claude", "plugin", "marketplace", "add", "steveyegge/beads"}
	if !reflect.DeepEqual(r.calls[0], want) {
		t.Errorf("call = %v, want %v", r.calls[0], want)
	}
}

func TestClaudeAddMarketplaceHTTPSPassesThrough(t *testing.T) {
	// Full URLs are accepted by claude as-is; driver must not mangle them.
	r := &fakeRunner{}
	d := claude.New(r)
	err := d.AddMarketplace(provision.Context{}, provision.Marketplace{
		Key:    "https://gitlab.com/org/repo",
		Source: "https://gitlab.com/org/repo",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"claude", "plugin", "marketplace", "add", "https://gitlab.com/org/repo"}
	if !reflect.DeepEqual(r.calls[0], want) {
		t.Errorf("call = %v, want %v", r.calls[0], want)
	}
}

func TestClaudeRemoveMarketplace(t *testing.T) {
	r := &fakeRunner{}
	d := claude.New(r)
	if err := d.RemoveMarketplace(provision.Context{}, "beads-marketplace"); err != nil {
		t.Fatal(err)
	}
	want := []string{"claude", "plugin", "marketplace", "remove", "beads-marketplace"}
	if !reflect.DeepEqual(r.calls[0], want) {
		t.Errorf("call = %v", r.calls[0])
	}
}

func TestClaudeInstalledPluginsBinaryMissingTolerated(t *testing.T) {
	r := &fakeRunner{err: context.Canceled}
	d := claude.New(r)
	got, err := d.InstalledPlugins(provision.Context{})
	if err != nil {
		t.Errorf("missing binary should be tolerated: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty slice, got %v", got)
	}
}
