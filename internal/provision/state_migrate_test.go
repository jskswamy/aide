package provision_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/provision"
)

// legacyStateV1 is a managed.json written before profiles: ownership
// keyed under "contexts".
const legacyStateV1 = `{
  "version": 1,
  "contexts": {
    "work": {
      "config_hash": "sha256:abc",
      "secrets_hash": "sha256:def",
      "agent_dir": "/home/u/.claude-work",
      "plugins": {"fmt@market": {"version": "1.2.0"}},
      "mcp_servers": {"git": {}},
      "marketplaces": {"market": {"source": "org/market"}},
      "hooks": [{"event": "PreToolUse", "matcher": "Bash", "command": "/bin/guard"}]
    }
  }
}`

func writeState(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "managed.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadStateReadsLegacyContextsKey(t *testing.T) {
	st, err := provision.LoadState(writeState(t, legacyStateV1))
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	cs := st.Contexts["work"]
	if cs == nil {
		t.Fatalf("legacy contexts entry lost: %+v", st.Contexts)
	}
	if cs.ConfigHash != "sha256:abc" || cs.SecretsHash != "sha256:def" || cs.AgentDir != "/home/u/.claude-work" {
		t.Errorf("hashes/agent dir not kept: %+v", cs)
	}
	if _, ok := cs.Plugins["fmt@market"]; !ok {
		t.Errorf("plugin ownership lost: %+v", cs.Plugins)
	}
	if _, ok := cs.MCPServers["git"]; !ok {
		t.Errorf("mcp ownership lost: %+v", cs.MCPServers)
	}
	if cs.Marketplaces["market"].Source != "org/market" {
		t.Errorf("marketplace ownership lost: %+v", cs.Marketplaces)
	}
	if len(cs.Hooks) != 1 || cs.Hooks[0].Command != "/bin/guard" {
		t.Errorf("hook ownership lost: %+v", cs.Hooks)
	}
}

func TestSaveStateWritesOnlyProfilesKey(t *testing.T) {
	path := writeState(t, legacyStateV1)
	st, err := provision.LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := provision.SaveState(path, st); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["contexts"]; ok {
		t.Errorf("saved state still has a contexts key:\n%s", data)
	}
	if _, ok := raw["profiles"]; !ok {
		t.Errorf("saved state has no profiles key:\n%s", data)
	}
	if v := string(raw["version"]); v != "2" {
		t.Errorf("saved version = %s, want 2", v)
	}
	again, err := provision.LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	if again.Contexts["work"] == nil || again.Contexts["work"].ConfigHash != "sha256:abc" {
		t.Errorf("round trip lost the entry: %+v", again.Contexts)
	}
}

// A file holding both keys (hand-edited) keeps every entry; the
// profiles entry wins for a name in both.
func TestLoadStateMergesBothKeys(t *testing.T) {
	st, err := provision.LoadState(writeState(t, `{"version": 2,
  "profiles": {"work": {"config_hash": "sha256:new"}},
  "contexts": {"work": {"config_hash": "sha256:old"}, "home": {"config_hash": "sha256:home"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := st.Contexts["work"].ConfigHash; got != "sha256:new" {
		t.Errorf("work hash = %q, want the profiles entry", got)
	}
	if st.Contexts["home"] == nil {
		t.Errorf("contexts-only entry dropped: %+v", st.Contexts)
	}
}

func TestLoadStateRejectsNewerVersion(t *testing.T) {
	path := writeState(t, `{"version": 99, "profiles": {}}`)
	_, err := provision.LoadState(path)
	if !errors.Is(err, provision.ErrStateVersion) {
		t.Fatalf("err = %v, want ErrStateVersion", err)
	}
	if !strings.Contains(err.Error(), "upgrade aide") || !strings.Contains(err.Error(), path) {
		t.Errorf("error lacks the file and remedy: %v", err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != `{"version": 99, "profiles": {}}` {
		t.Errorf("newer state file was modified: %s", data)
	}
}

// Rewriting config.yaml for a migration changes its bytes; profiles
// synced against the old bytes move to the new hash, while a profile
// that had already drifted keeps its stale hash and stays drifted.
func TestRehashConfigMovesOnlyInSyncProfiles(t *testing.T) {
	path := writeState(t, `{"version": 1, "contexts": {
  "work": {"config_hash": "sha256:old", "plugins": {"p@m": {}}},
  "stale": {"config_hash": "sha256:older"}}}`)
	n, err := provision.RehashConfig(path, "sha256:old", "sha256:new")
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("rehashed %d profiles, want 1", n)
	}
	st, err := provision.LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := st.Contexts["work"].ConfigHash; got != "sha256:new" {
		t.Errorf("work hash = %q", got)
	}
	if _, ok := st.Contexts["work"].Plugins["p@m"]; !ok {
		t.Error("ownership lost while rehashing")
	}
	if got := st.Contexts["stale"].ConfigHash; got != "sha256:older" {
		t.Errorf("drifted profile rehashed: %q", got)
	}
}

func TestRehashConfigWithoutStateWritesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "managed.json")
	n, err := provision.RehashConfig(path, "sha256:old", "sha256:new")
	if err != nil || n != 0 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("state file created: %v", err)
	}
}
