package provision_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/provision"
)

// minimal config: one context with one declared plugin. The
// declare-only shape is sufficient for desired-set computation; the
// drift check only cares about key membership, not shape.
func driftTestConfig(t *testing.T, dir string, contextName string, plugins []string) (*config.Config, string) {
	t.Helper()
	cfgPath := filepath.Join(dir, "config.yaml")
	body := "agents:\n  claude:\n    binary: claude\ncontexts:\n  " + contextName + ":\n    agent: claude\n"
	if len(plugins) > 0 {
		body += "plugins:\n"
		for _, p := range plugins {
			body += "  " + p + ": ~\n"
		}
	}
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(dir, dir)
	if err != nil {
		t.Fatal(err)
	}
	return cfg, cfgPath
}

func TestDriftPerContextHash(t *testing.T) {
	dir := t.TempDir()
	cfg, cfgPath := driftTestConfig(t, dir, "alpha", nil)
	statePath := filepath.Join(dir, "managed.json")
	h, _ := provision.ConfigHash(cfgPath)
	_ = provision.SaveState(statePath, &provision.ManagedState{
		Version: 1,
		Contexts: map[string]*provision.ContextState{
			"alpha": {ConfigHash: h, SyncedAt: time.Now()},
		},
	})

	got, err := provision.DriftStatus(cfg, cfgPath, statePath, "alpha", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if got != provision.DriftNone {
		t.Errorf("expected DriftNone immediately after sync, got %v", got)
	}

	_ = os.WriteFile(cfgPath, []byte("agents:\n  claude:\n    binary: claude\ncontexts:\n  alpha:\n    agent: claude\n  beta:\n    agent: claude\n"), 0o600)
	cfg2, err := config.Load(dir, dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err = provision.DriftStatus(cfg2, cfgPath, statePath, "alpha", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if got != provision.DriftConfigChanged {
		t.Errorf("expected DriftConfigChanged after config edit, got %v", got)
	}
}

// Bug case: when default-context sync writes its own hash, prod's
// drift status must NOT switch to "none". Pre-fix, ConfigHash lived
// at state root so default's sync clobbered prod's signal.
func TestDriftPerContextIsolatedFromOtherContexts(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	body := "agents:\n  claude:\n    binary: claude\ncontexts:\n  default:\n    agent: claude\n  prod:\n    agent: claude\nplugins:\n  jskswamy/aide: ~\n"
	_ = os.WriteFile(cfgPath, []byte(body), 0o600)
	cfg, err := config.Load(dir, dir)
	if err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(dir, "managed.json")
	h, _ := provision.ConfigHash(cfgPath)
	// default synced, prod never did.
	_ = provision.SaveState(statePath, &provision.ManagedState{
		Version: 1,
		Contexts: map[string]*provision.ContextState{
			"default": {
				ConfigHash:   h,
				SyncedAt:     time.Now(),
				Marketplaces: map[string]provision.ManagedItem{"jskswamy/aide": {InstalledAt: time.Now()}},
			},
		},
	})

	got, err := provision.DriftStatus(cfg, cfgPath, statePath, "prod", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if got != provision.DriftNeverSynced {
		t.Errorf("expected DriftNeverSynced for prod (default's sync should not affect prod), got %v", got)
	}
}

// Per-context shortfall: state.Contexts[ctx] exists, hash matches,
// but the managed set is smaller than desired (e.g. user added a
// plugin to config without running sync). DriftStatus should still
// fire DriftConfigChanged because there is install work pending.
func TestDriftShortfallWhenManagedSetIncomplete(t *testing.T) {
	dir := t.TempDir()
	cfg, cfgPath := driftTestConfig(t, dir, "alpha", []string{"foo/marketplace-a", "foo/marketplace-b"})
	statePath := filepath.Join(dir, "managed.json")
	h, _ := provision.ConfigHash(cfgPath)
	// state records only one of the two declared marketplaces.
	_ = provision.SaveState(statePath, &provision.ManagedState{
		Version: 1,
		Contexts: map[string]*provision.ContextState{
			"alpha": {
				ConfigHash:   h,
				SyncedAt:     time.Now(),
				Marketplaces: map[string]provision.ManagedItem{"foo/marketplace-a": {InstalledAt: time.Now()}},
			},
		},
	})

	got, err := provision.DriftStatus(cfg, cfgPath, statePath, "alpha", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if got != provision.DriftConfigChanged {
		t.Errorf("expected DriftConfigChanged for shortfall, got %v", got)
	}
}

func TestDriftShortfallWhenHookNotSynced(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	body := "agents:\n  claude:\n    binary: claude\ncontexts:\n  alpha:\n    agent: claude\nhooks:\n  pre_tool:\n    - command: rtk hook claude\n"
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(dir, dir)
	if err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(dir, "managed.json")
	h, _ := provision.ConfigHash(cfgPath)
	// Hash matches (looks current) but no hooks in managed state.
	_ = provision.SaveState(statePath, &provision.ManagedState{
		Version: 1,
		Contexts: map[string]*provision.ContextState{
			"alpha": {ConfigHash: h, SyncedAt: time.Now()},
		},
	})

	got, err := provision.DriftStatus(cfg, cfgPath, statePath, "alpha", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if got != provision.DriftConfigChanged {
		t.Errorf("expected DriftConfigChanged for hook shortfall, got %v", got)
	}
}

func TestDriftMissingStateMeansNeverSynced(t *testing.T) {
	dir := t.TempDir()
	cfg, cfgPath := driftTestConfig(t, dir, "alpha", nil)
	got, err := provision.DriftStatus(cfg, cfgPath, filepath.Join(dir, "absent.json"), "alpha", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if got != provision.DriftNeverSynced {
		t.Errorf("expected DriftNeverSynced, got %v", got)
	}
}

// TestDriftStatusAgentDirHookNoFalsePositive verifies that when a hook
// command uses {agent_dir} in config and managed state records the resolved
// path, DriftStatus returns DriftNone (not DriftConfigChanged).
// Without the fix (passing agentDir to ResolveDesired), the desired set
// contains "{agent_dir}/hooks/my-hook" which never matches the managed
// "/home/user/.claude/hooks/my-hook", producing a false positive.
func TestDriftStatusAgentDirHookNoFalsePositive(t *testing.T) {
	fakeHome := "/home/user"
	fakeAgentDir := fakeHome + "/.claude"

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	body := "agents:\n  claude:\n    binary: claude\ncontexts:\n  alpha:\n    agent: claude\nhooks:\n  pre_tool:\n    - command: \"{agent_dir}/hooks/my-hook\"\n"
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(dir, dir)
	if err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(dir, "managed.json")
	h, _ := provision.ConfigHash(cfgPath)
	// Managed state records the resolved absolute path (as sync would write it).
	resolvedCmd := fakeAgentDir + "/hooks/my-hook"
	_ = provision.SaveState(statePath, &provision.ManagedState{
		Version: 1,
		Contexts: map[string]*provision.ContextState{
			"alpha": {
				ConfigHash: h,
				SyncedAt:   time.Now(),
				Hooks: []provision.ManagedHook{
					{Event: "pre_tool", Command: resolvedCmd},
				},
			},
		},
	})

	got, err := provision.DriftStatus(cfg, cfgPath, statePath, "alpha", fakeAgentDir, fakeHome)
	if err != nil {
		t.Fatal(err)
	}
	if got != provision.DriftNone {
		t.Errorf("expected DriftNone (no false positive for {agent_dir} hook), got %v", got)
	}
}
