package provision_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jskswamy/aide/internal/provision"
)

func TestLoadStateMissingFileReturnsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "managed.json")
	st, err := provision.LoadState(path)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if st == nil {
		t.Fatal("expected non-nil state")
		return
	}
	if st.Contexts == nil {
		t.Error("expected non-nil Contexts map")
	}
}

func TestSaveStateRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "managed.json")
	st := &provision.ManagedState{
		Version: 1,
		Contexts: map[string]*provision.ContextState{
			"work": {
				ConfigHash: "sha256:abc",
				Plugins:    map[string]provision.ManagedItem{"linear": {Version: "1.2"}},
				MCPServers: map[string]provision.ManagedItem{"postgres": {}},
			},
		},
	}
	if err := provision.SaveState(path, st); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	got, err := provision.LoadState(path)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if got.Contexts["work"].ConfigHash != "sha256:abc" {
		t.Errorf("ConfigHash = %q", got.Contexts["work"].ConfigHash)
	}
	if got.Contexts["work"].Plugins["linear"].Version != "1.2" {
		t.Errorf("plugin version not preserved: %+v", got.Contexts["work"])
	}
}

func TestSaveStatePermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "managed.json")
	if err := provision.SaveState(path, &provision.ManagedState{Version: 1}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("perm = %o, want 0o600", perm)
	}
}
