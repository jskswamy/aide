package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/provision"
	"github.com/jskswamy/aide/internal/provision/provisiontest"
)

// hookFakeProv is a hook-capable provisioner registered under "fakehookagent"
// for integration tests that exercise the full sync lifecycle.
var hookFakeProv = &provisiontest.FakeProvisionerWithHookInstaller{
	FakeProvisioner: &provisiontest.FakeProvisioner{
		AgentName:        "fakehookagent",
		SupportsHooksCfg: true,
	},
}

func init() {
	provision.RegisterProvisioner(hookFakeProv)
}

func hookFakeReset(t *testing.T) {
	t.Helper()
	hookFakeProv.Reset()
	hookFakeProv.StoredHooks = nil
	hookFakeProv.WriteHooksCalls = nil
}

// writeHookConfig writes a config.yaml with the given top-level hook commands
// bound to "fakehookagent" and the current working directory.
func writeHookConfig(t *testing.T, dir string, hookCmds ...string) {
	t.Helper()
	cwd, _ := os.Getwd()
	var b strings.Builder
	b.WriteString("agents:\n  fakehookagent:\n    binary: echo\n")
	b.WriteString("contexts:\n  work:\n    agent: fakehookagent\n    match:\n      - path: ")
	b.WriteString(cwd)
	b.WriteString("\n")
	if len(hookCmds) > 0 {
		b.WriteString("hooks:\n  pre_tool:\n")
		for _, cmd := range hookCmds {
			b.WriteString("    - command: ")
			b.WriteString(cmd)
			b.WriteString("\n")
		}
	}
	path := filepath.Join(dir, "xdg", "aide", "config.yaml")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestHookSyncLifecycle exercises the full add → sync → list → remove → sync path.
func TestHookSyncLifecycle(t *testing.T) {
	hookFakeReset(t)
	dir := isolatedConfigDir(t)
	writeHookConfig(t, dir)

	// 1. Add a hook.
	cmd := hookAddCmd()
	cmd.SetArgs([]string{"--context", "work", "--event", "pre_tool", "--command", "rtk hook {agent}"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("hook add: %v", err)
	}

	// 2. Sync — should install the hook.
	out, err := runSyncCmd(t, "", "--context", "work", "--yes")
	if err != nil {
		t.Fatalf("first sync: %v\n%s", err, out)
	}
	if !strings.Contains(out, "install") || !strings.Contains(out, "hook") {
		t.Errorf("expected install hook in plan output, got:\n%s", out)
	}
	if !strings.Contains(out, "Sync complete") {
		t.Errorf("expected Sync complete, got:\n%s", out)
	}

	// Hook should now be in the fake agent's stored state.
	if len(hookFakeProv.StoredHooks) != 1 || hookFakeProv.StoredHooks[0].Command != "rtk hook fakehookagent" {
		t.Errorf("StoredHooks = %+v, want [{..rtk hook fakehookagent..}]", hookFakeProv.StoredHooks)
	}

	// 3. List — hook should appear as managed (✓).
	var listBuf bytes.Buffer
	listCmd := hookListCmd()
	listCmd.SetOut(&listBuf)
	listCmd.SetArgs([]string{"--context", "work"})
	if err := listCmd.Execute(); err != nil {
		t.Fatalf("hook list: %v", err)
	}
	if !strings.Contains(listBuf.String(), "✓") {
		t.Errorf("expected managed ✓ in list output:\n%s", listBuf.String())
	}
	if !strings.Contains(listBuf.String(), "rtk hook fakehookagent") {
		t.Errorf("expected resolved command in list:\n%s", listBuf.String())
	}

	// 4. Second sync — no-op.
	out2, err := runSyncCmd(t, "", "--context", "work", "--yes")
	if err != nil {
		t.Fatalf("second sync: %v\n%s", err, out2)
	}
	if !strings.Contains(out2, "no changes") {
		t.Errorf("expected no-op on second sync, got:\n%s", out2)
	}

	// 5. Remove the hook using the resolved command form.
	rmCmd := hookRemoveCmd()
	rmCmd.SetArgs([]string{"--context", "work", "--event", "pre_tool", "--command", "rtk hook fakehookagent"})
	if err := rmCmd.Execute(); err != nil {
		t.Fatalf("hook remove (resolved form): %v", err)
	}

	// 6. Sync — should uninstall the hook.
	out3, err := runSyncCmd(t, "", "--context", "work", "--yes")
	if err != nil {
		t.Fatalf("uninstall sync: %v\n%s", err, out3)
	}
	if !strings.Contains(out3, "uninstall") || !strings.Contains(out3, "hook") {
		t.Errorf("expected uninstall hook in plan, got:\n%s", out3)
	}

	// Hook should be gone from the fake agent's stored state.
	if len(hookFakeProv.StoredHooks) != 0 {
		t.Errorf("StoredHooks should be empty after uninstall, got %+v", hookFakeProv.StoredHooks)
	}

	// managed.json should have no hooks recorded.
	st, err := provision.LoadState(provision.DefaultStatePath(dir))
	if err != nil {
		t.Fatal(err)
	}
	if cs := st.Contexts["work"]; cs != nil && len(cs.Hooks) != 0 {
		t.Errorf("managed.json still has hooks after uninstall: %+v", cs.Hooks)
	}
}

// TestHookRemoveAcceptsBothRawAndResolvedCommand verifies that hook remove
// works whether the user types the raw template or the resolved agent name.
func TestHookRemoveAcceptsBothRawAndResolvedCommand(t *testing.T) {
	for _, tc := range []struct {
		name    string
		rmCmd   string
	}{
		{"raw template", "rtk hook {agent}"},
		{"resolved form", "rtk hook fakehookagent"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hookFakeReset(t)
			dir := isolatedConfigDir(t)
			writeHookConfig(t, dir)

			add := hookAddCmd()
			add.SetArgs([]string{"--context", "work", "--event", "pre_tool", "--command", "rtk hook {agent}"})
			if err := add.Execute(); err != nil {
				t.Fatalf("add: %v", err)
			}

			rm := hookRemoveCmd()
			rm.SetArgs([]string{"--context", "work", "--event", "pre_tool", "--command", tc.rmCmd})
			if err := rm.Execute(); err != nil {
				t.Fatalf("remove with %q: %v", tc.rmCmd, err)
			}

			// Config should no longer contain the hook.
			data, _ := os.ReadFile(filepath.Join(dir, "xdg", "aide", "config.yaml"))
			if strings.Contains(string(data), "rtk hook") {
				t.Errorf("hook still in config.yaml after remove:\n%s", data)
			}
		})
	}
}

// TestHookRemoveByMatcherRemovesCorrectEntry verifies that --matcher selects
// exactly the matching entry when two hooks share the same event and command.
func TestHookRemoveByMatcherRemovesCorrectEntry(t *testing.T) {
	hookFakeReset(t)
	dir := isolatedConfigDir(t)
	writeHookConfig(t, dir)

	for _, matcher := range []string{"", "shell"} {
		add := hookAddCmd()
		add.SetArgs([]string{"--context", "work", "--event", "pre_tool",
			"--matcher", matcher, "--command", "multi-hook"})
		if err := add.Execute(); err != nil {
			t.Fatalf("add matcher=%q: %v", matcher, err)
		}
	}

	// Remove only the "shell" matcher entry.
	rm := hookRemoveCmd()
	rm.SetArgs([]string{"--context", "work", "--event", "pre_tool",
		"--matcher", "shell", "--command", "multi-hook"})
	if err := rm.Execute(); err != nil {
		t.Fatalf("remove: %v", err)
	}

	// The blank-matcher entry should still be present; the shell one should be gone.
	var listBuf bytes.Buffer
	lc := hookListCmd()
	lc.SetOut(&listBuf)
	lc.SetArgs([]string{"--context", "work"})
	_ = lc.Execute()
	out := listBuf.String()
	lines := strings.Count(out, "multi-hook")
	if lines != 1 {
		t.Errorf("expected 1 remaining multi-hook entry, got %d:\n%s", lines, out)
	}
}
