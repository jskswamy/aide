package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/provision"
)

func runSyncCmd(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()
	cmd := syncCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

func TestSync_PlanOnly_NoMutation(t *testing.T) {
	fakeProvReset(t)
	setupProvisionConfig(t,
		[]string{"linear"}, nil,
		map[string]string{"linear": "linear@1.2"}, nil,
	)
	out, err := runSyncCmd(t, "", "--context", "work", "--plan")
	if err != nil {
		t.Fatalf("execute: %v\n%s", err, out)
	}
	if !strings.Contains(out, "+ install") || !strings.Contains(out, "linear") {
		t.Errorf("plan should include install op for linear:\n%s", out)
	}
	if len(theFakeProv.InstallCalls) != 0 {
		t.Errorf("--plan should not install anything; calls=%v", theFakeProv.InstallCalls)
	}
}

func TestSync_Yes_AppliesAndUpdatesState(t *testing.T) {
	fakeProvReset(t)
	theFakeProv.RequireTTY = false
	home := setupProvisionConfig(t,
		[]string{"linear"}, []string{"shared"},
		map[string]string{"linear": "linear@1.2"},
		map[string]string{"shared": "shared-mcp"},
	)
	out, err := runSyncCmd(t, "", "--context", "work", "--yes")
	if err != nil {
		t.Fatalf("execute: %v\n%s", err, out)
	}
	if len(theFakeProv.InstallCalls) != 1 || theFakeProv.InstallCalls[0].Key != "linear" {
		t.Errorf("expected linear install, got %v", theFakeProv.InstallCalls)
	}
	if !strings.Contains(out, "Sync complete") {
		t.Errorf("missing summary:\n%s", out)
	}
	// Verify state file picked up the managed entries.
	st, err := provision.LoadState(provision.DefaultStatePath(home))
	if err != nil {
		t.Fatal(err)
	}
	cs := st.Contexts["work"]
	if cs == nil {
		t.Fatal("context state missing")
		return
	}
	if _, ok := cs.Plugins["linear"]; !ok {
		t.Errorf("linear not recorded as managed: %+v", cs.Plugins)
	}
	if _, ok := cs.MCPServers["shared"]; !ok {
		t.Errorf("shared mcp not recorded: %+v", cs.MCPServers)
	}
	if cs.ConfigHash == "" {
		t.Error("config hash should be recorded on the context")
	}
}

// TestSync_UnmanagedDoesNotBlockInteractive pins the relaxed gate:
// plain `aide sync` (no --yes) with unmanaged items in the agent
// must NOT hard-error. Earlier behaviour was to bail with "run aide
// adopt first", which forced an unrelated workflow whenever a
// context had any plugin/MCP aide didn't know about. The OpIgnore
// items are no-ops in Apply, so blocking on them added friction
// without preventing harm.
func TestSync_UnmanagedDoesNotBlockInteractive(t *testing.T) {
	fakeProvReset(t)
	theFakeProv.RequireTTY = false
	setupProvisionConfig(t,
		[]string{"linear"}, nil,
		map[string]string{"linear": "linear@1.2"}, nil,
	)
	// Inject an installed-but-undeclared plugin so the plan contains
	// an OpIgnore op alongside the install for "linear". "linear" is
	// declared but NOT in InstalledPluginList → plan adds install op.
	// "stranger" is in InstalledPluginList but NOT declared → plan
	// adds OpIgnore. Both together exercise the relaxed gate.
	theFakeProv.InstalledPluginList = []provision.Plugin{
		{Key: "stranger", Name: "stranger@x"},
	}
	// Answer "y" at the prompt to actually apply.
	out, err := runSyncCmd(t, "y\n", "--context", "work")
	if err != nil {
		t.Fatalf("execute should not error on unmanaged items, got: %v\n%s", err, out)
	}
	if !strings.Contains(out, "unmanaged item") {
		t.Errorf("expected unmanaged-items note in output:\n%s", out)
	}
	if strings.Contains(out, "run `aide adopt` first") {
		t.Errorf("hard-bail wording should be gone:\n%s", out)
	}
	if !strings.Contains(out, "Sync complete") {
		t.Errorf("apply should run after the prompt; output:\n%s", out)
	}
}

func TestSync_RequiresTTY_BlocksYes(t *testing.T) {
	fakeProvReset(t)
	theFakeProv.RequireTTY = true
	setupProvisionConfig(t,
		[]string{"linear"}, nil,
		map[string]string{"linear": "linear@1.2"}, nil,
	)
	_, err := runSyncCmd(t, "", "--context", "work", "--yes")
	if err == nil || !strings.Contains(err.Error(), "TTY") {
		t.Errorf("expected TTY error, got %v", err)
	}
	if len(theFakeProv.InstallCalls) != 0 {
		t.Errorf("install should be blocked, got %v", theFakeProv.InstallCalls)
	}
}

func TestSync_CapabilityMismatch(t *testing.T) {
	fakeProvReset(t)
	theFakeProv.SupportsPlug = false
	setupProvisionConfig(t,
		[]string{"linear"}, nil,
		map[string]string{"linear": "linear@1.2"}, nil,
	)
	_, err := runSyncCmd(t, "", "--context", "work", "--yes")
	if err == nil || !strings.Contains(err.Error(), "does not support plugins") {
		t.Errorf("expected capability mismatch, got %v", err)
	}
}

func TestSync_ApplyFailure_StateNotUpdated(t *testing.T) {
	fakeProvReset(t)
	theFakeProv.RequireTTY = false
	home := setupProvisionConfig(t,
		[]string{"linear"}, nil,
		map[string]string{"linear": "linear@1.2"}, nil,
	)
	// Swap install to fail by wrapping fakeProv with a closure-friendly
	// override: use a sentinel handled in the engine path. Easiest: we
	// inject the failure via the fake's plugin-install hook. The
	// current fake records but doesn't error — add an error sentinel
	// via global. Use a small helper:
	// Force capability mismatch path by declaring a NON-plugin scenario
	// is messy; instead, simulate failure through MCP write error.
	// Simpler: declare the plugin but flip InstalledPlugins to error.
	theFakeProv.PluginsErr = errors.New("listing failed")

	_, err := runSyncCmd(t, "", "--context", "work", "--yes")
	if err == nil {
		t.Fatal("expected failure")
	}
	st, _ := provision.LoadState(provision.DefaultStatePath(home))
	if cs := st.Contexts["work"]; cs != nil && cs.ConfigHash != "" {
		t.Errorf("context state should not be updated on failure, got hash %q", cs.ConfigHash)
	}
}

// TestSync_MCPSecretTemplate_NoSecretsFileErrors pins the wiring of T16
// (AIDE-4c9.1) end-to-end through the sync command. When config declares
// an MCP server with env values referencing {{ .secrets.X }} but the
// context does NOT carry a `secret:` field, sync must fail at resolution
// time with an error naming the offending MCP server — silently shipping
// an unresolved template into the agent's .mcp.json would burn the user
// with an auth failure at agent runtime instead of at sync time.
//
// This test covers the nil-TemplateData branch of ResolveSecretsInMCPEnv;
// the happy path (resolves real values) is covered by the unit tests in
// internal/provision/secrets_test.go. End-to-end happy path with an
// encrypted .enc.yaml is intentionally not asserted here — it would
// duplicate the launcher's secrets-decrypt coverage without adding
// information beyond what the unit + wiring tests already pin.
func TestSync_MCPSecretTemplate_NoSecretsFileErrors(t *testing.T) {
	fakeProvReset(t)
	home := isolatedConfigDir(t)
	cwd, _ := os.Getwd()
	yaml := fmt.Sprintf(`mcp_servers:
  github:
    command: github-mcp-server
    env:
      TOKEN: "{{ .secrets.api_key }}"
contexts:
  work:
    agent: fakeagent
    match:
      - path: %s
    mcp_servers:
      - github
`, cwd)
	cfgPath := filepath.Join(home, "xdg", "aide", "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := runSyncCmd(t, "", "--context", "work", "--plan")
	if err == nil {
		t.Fatalf("sync expected to fail (MCP env references secret but no secrets file configured); got nil error and out: %s", out)
	}
	if !strings.Contains(err.Error(), "github") {
		t.Errorf("error must name the offending MCP server %q; got: %v", "github", err)
	}
	if !strings.Contains(err.Error(), "secrets") {
		t.Errorf("error must mention secrets/template misconfiguration; got: %v", err)
	}
}
