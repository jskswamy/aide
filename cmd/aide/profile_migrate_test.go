package main

import (
	"bytes"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/provision"
)

const migrateFixtureConfig = `# my aide config
agents:
  claude:
    binary: claude
contexts:
  work:
    agent: claude
    secret: work
    profile: work
    match:
      - path: /never/matches/anything
    env:
      ANTHROPIC_API_KEY: "{{ .secrets.anthropic_key }}"
    plugins:
      extra:
        my-org/internal: [private-tool]
  personal:
    agent: claude
default_context: personal
plugins:
  steveyegge/beads: [beads]
`

func execProfileMigrate(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	cmd := profileCmd()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs(append([]string{"migrate"}, args...))
	err := cmd.Execute()
	return out.String(), errOut.String(), err
}

func execConfigMigrate(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	cmd := configCmd()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs(append([]string{"migrate"}, args...))
	err := cmd.Execute()
	return out.String(), errOut.String(), err
}

// migrateFixture writes the legacy config and a populated state file
// recording a successful sync of every profile against it.
func migrateFixture(t *testing.T) (home, cfgPath, statePath string) {
	t.Helper()
	home = isolatedConfigDir(t)
	cfgPath = filepath.Join(home, "xdg", "aide", "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(migrateFixtureConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(filepath.Dir(cfgPath), home)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := provision.ConfigHash(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	st := &provision.ManagedState{Version: 1, Contexts: map[string]*provision.ContextState{}}
	for _, name := range []string{"work", "personal"} {
		desired, err := provision.ResolveDesired(cfg, name, "", home)
		if err != nil {
			t.Fatal(err)
		}
		cs := &provision.ContextState{
			ConfigHash:   hash,
			SyncedAt:     time.Now(),
			AgentDir:     filepath.Join(home, ".claude-"+name),
			Plugins:      map[string]provision.ManagedItem{},
			Marketplaces: map[string]provision.ManagedItem{},
			MCPServers:   map[string]provision.ManagedItem{},
		}
		for k := range desired.Plugins {
			cs.Plugins[k] = provision.ManagedItem{Version: "1.0.0"}
		}
		for k := range desired.Marketplaces {
			cs.Marketplaces[k] = provision.ManagedItem{}
		}
		for k := range desired.MCPServers {
			cs.MCPServers[k] = provision.ManagedItem{}
		}
		st.Contexts[name] = cs
	}
	statePath = provision.DefaultStatePath(home)
	if err := provision.SaveState(statePath, st); err != nil {
		t.Fatal(err)
	}
	return home, cfgPath, statePath
}

func driftOf(t *testing.T, home, cfgPath, statePath, name string) provision.DriftKind {
	t.Helper()
	cfg, err := config.Load(filepath.Dir(cfgPath), home)
	if err != nil {
		t.Fatalf("loading config: %v", err)
	}
	d, err := provision.DriftStatus(cfg, cfgPath, statePath, name, "", home)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestProfileMigrate_PreviewWritesNothing(t *testing.T) {
	_, cfgPath, statePath := migrateFixture(t)
	stateBefore, _ := os.ReadFile(statePath)

	out, _, err := execProfileMigrate(t)
	if err != nil {
		t.Fatalf("migrate: %v\n%s", err, out)
	}
	for _, want := range []string{"profiles:", "default_profile: personal", "config_dir_name: work",
		"credentials:", "nothing written", "--write", "personal"} {
		if !strings.Contains(out, want) {
			t.Errorf("preview missing %q:\n%s", want, out)
		}
	}
	if got, _ := os.ReadFile(cfgPath); string(got) != migrateFixtureConfig {
		t.Errorf("preview modified config.yaml:\n%s", got)
	}
	if got, _ := os.ReadFile(statePath); !bytes.Equal(got, stateBefore) {
		t.Error("preview modified provisioning state")
	}
	if _, err := os.Stat(cfgPath + ".bak"); !os.IsNotExist(err) {
		t.Errorf("preview wrote a backup: %v", err)
	}
}

// Acceptance: a populated state migrates with no managed resource
// treated as new and no false drift banner.
func TestProfileMigrate_WriteKeepsOwnershipWithoutFalseDrift(t *testing.T) {
	home, cfgPath, statePath := migrateFixture(t)
	for _, name := range []string{"work", "personal"} {
		if d := driftOf(t, home, cfgPath, statePath, name); d != provision.DriftNone {
			t.Fatalf("fixture %s already drifted: %v", name, d)
		}
	}
	before, err := provision.LoadState(statePath)
	if err != nil {
		t.Fatal(err)
	}

	out, _, err := execProfileMigrate(t, "--write")
	if err != nil {
		t.Fatalf("migrate --write: %v\n%s", err, out)
	}

	bak, err := os.ReadFile(cfgPath + ".bak")
	if err != nil || string(bak) != migrateFixtureConfig {
		t.Fatalf("backup missing or wrong (%v):\n%s", err, bak)
	}
	written, _ := os.ReadFile(cfgPath)
	if strings.Contains(string(written), "contexts:") || strings.Contains(string(written), ".secrets.") ||
		!strings.Contains(string(written), "# my aide config") {
		t.Errorf("config.yaml not migrated or comment lost:\n%s", written)
	}

	after, err := provision.LoadState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"work", "personal"} {
		b, a := before.Contexts[name], after.Contexts[name]
		if a == nil {
			t.Fatalf("%s ownership lost", name)
		}
		if !slices.Equal(slices.Sorted(maps.Keys(a.Plugins)), slices.Sorted(maps.Keys(b.Plugins))) ||
			!slices.Equal(slices.Sorted(maps.Keys(a.Marketplaces)), slices.Sorted(maps.Keys(b.Marketplaces))) ||
			a.AgentDir != b.AgentDir {
			t.Errorf("%s ownership changed: before %+v after %+v", name, b, a)
		}
		if d := driftOf(t, home, cfgPath, statePath, name); d != provision.DriftNone {
			t.Errorf("%s reports drift %v after migration", name, d)
		}
	}
	if len(before.Contexts["work"].Plugins) == 0 {
		t.Fatal("fixture records no managed plugins; the test proves nothing")
	}
}

func TestConfigMigrate_PreviewAndWriteSplitFiles(t *testing.T) {
	home, cfgPath, statePath := migrateFixture(t)

	// Preview shows split file headers without writing
	out, _, err := execConfigMigrate(t)
	if err != nil {
		t.Fatalf("config migrate preview: %v\n%s", err, out)
	}
	for _, want := range []string{"# === config.yaml ===", "# === provisioning.yaml ===", "# === profiles/work.yaml ===", "# === profiles/personal.yaml ==="} {
		if !strings.Contains(out, want) {
			t.Errorf("preview missing %q:\n%s", want, out)
		}
	}

	// Write applies the modular split and rehashes state
	out, _, err = execConfigMigrate(t, "--write")
	if err != nil {
		t.Fatalf("config migrate --write: %v\n%s", err, out)
	}
	if _, err := os.Stat(cfgPath + ".bak"); err != nil {
		t.Errorf("expected backup file: %v", err)
	}
	for _, rel := range []string{"config.yaml", "provisioning.yaml", "profiles/work.yaml", "profiles/personal.yaml"} {
		if _, err := os.Stat(filepath.Join(filepath.Dir(cfgPath), rel)); err != nil {
			t.Errorf("missing %s after config migrate --write: %v", rel, err)
		}
	}
	for _, name := range []string{"work", "personal"} {
		if d := driftOf(t, home, cfgPath, statePath, name); d != provision.DriftNone {
			t.Errorf("%s reports drift %v after config migrate --write", name, d)
		}
	}
}

func TestProfileMigrate_NothingToDo(t *testing.T) {
	_, cfgPath, _ := migrateFixture(t)
	if _, _, err := execProfileMigrate(t, "--write"); err != nil {
		t.Fatal(err)
	}
	migrated, _ := os.ReadFile(cfgPath)
	out, _, err := execProfileMigrate(t, "--write")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Nothing to migrate") {
		t.Errorf("second run output:\n%s", out)
	}
	if again, _ := os.ReadFile(cfgPath); !bytes.Equal(again, migrated) {
		t.Error("second run rewrote the config")
	}
}

func TestProfileMigrate_NeverTouchesProjectConfig(t *testing.T) {
	home, _, _ := migrateFixture(t)
	project := filepath.Join(home, ".aide.yaml")
	body := "agent: claude\n"
	if err := os.WriteFile(project, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := execProfileMigrate(t, "--write"); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(project); string(got) != body {
		t.Errorf(".aide.yaml modified:\n%s", got)
	}
}

const openCodeNotCopiedNote = "OpenCode accounts and sessions were not copied and were not deleted. " +
	"They remain in the database OpenCode was already opening, which is not always under the data directory."

// Any config that mentions OpenCode gets the not-copied note, whether
// or not anything else changes; a Claude-only config does not.
func TestProfileMigrate_OpenCodeNotCopiedNote(t *testing.T) {
	for name, tc := range map[string]struct {
		config string
		args   []string
	}{
		"legacy contexts preview": {"contexts:\n  work: {agent: opencode, profile: work}\ndefault_context: work\n", nil},
		"legacy contexts write":   {"contexts:\n  work: {agent: opencode, profile: work}\ndefault_context: work\n", []string{"--write"}},
		"already profiles":        {"profiles:\n  work: {agent: opencode, config_dir_name: work}\n", nil},
		"already profiles write":  {"profiles:\n  work: {agent: opencode, config_dir_name: work}\n", []string{"--write"}},
		"config_dirs key": {"profiles:\n  work:\n    agent: claude\n    config_dir_name: work\n" +
			"    config_dirs: {opencode: ~/oc-work}\n", nil},
		"global login row": {"profiles:\n  personal: {agent: opencode}\n", nil},
		"agents alias":     {"agents:\n  oc: {binary: opencode}\nprofiles:\n  work: {agent: oc, config_dir_name: work}\n", nil},
	} {
		t.Run(name, func(t *testing.T) {
			home := isolatedConfigDir(t)
			cfgPath := filepath.Join(home, "xdg", "aide", "config.yaml")
			if err := os.WriteFile(cfgPath, []byte(tc.config), 0o600); err != nil {
				t.Fatal(err)
			}
			out, _, err := execProfileMigrate(t, tc.args...)
			if err != nil {
				t.Fatalf("migrate: %v\n%s", err, out)
			}
			if !strings.Contains(out, openCodeNotCopiedNote) {
				t.Errorf("output missing the OpenCode note:\n%s", out)
			}
			if strings.Contains(out, ".local/share/opencode/opencode.db") {
				t.Errorf("output names the default database path:\n%s", out)
			}
		})
	}
}

// Preview, the --write summary and the unchanged second run all stay
// silent about OpenCode for a Claude-only config.
func TestProfileMigrate_ClaudeOnlyHasNoOpenCodeNote(t *testing.T) {
	migrateFixture(t)
	for _, args := range [][]string{nil, {"--write"}, {"--write"}} {
		out, _, err := execProfileMigrate(t, args...)
		if err != nil {
			t.Fatalf("migrate %v: %v\n%s", args, err, out)
		}
		if strings.Contains(out, openCodeNotCopiedNote) {
			t.Errorf("migrate %v printed an OpenCode note for a Claude-only config:\n%s", args, out)
		}
	}
}

func TestConfigMigrate_RefusesSharedNodesOnProfilesConfig(t *testing.T) {
	home := isolatedConfigDir(t)
	cfgPath := filepath.Join(home, "xdg", "aide", "config.yaml")
	body := "profiles:\n  work: &base\n    agent: claude\n    config_dir_name: work\n  personal:\n    <<: *base\n    config_dir_name: personal\n"
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := execConfigMigrate(t); err == nil || !errors.Is(err, config.ErrSharedYAML) {
		t.Fatalf("expected ErrSharedYAML from config migrate, got: %v", err)
	}
}

func TestConfigMigrate_ValidatesInlinePlugins(t *testing.T) {
	home := isolatedConfigDir(t)
	cfgPath := filepath.Join(home, "xdg", "aide", "config.yaml")
	body := "default_profile: default\nplugins:\n  invalid-repo-key:\n    - commit-tools\n"
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := execConfigMigrate(t); err == nil {
		t.Fatal("expected validation error for invalid inline config plugins")
	}
}
