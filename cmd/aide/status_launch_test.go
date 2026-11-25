package main

import (
	"os"
	"path/filepath"
	"testing"
)

// status describes the sandbox launch would apply: capability
// overrides count (the network capability opens the network, ssh adds
// its guard), and a subdirectory sees the same sandbox as the root.
func TestStatus_MatchesLaunchSandbox(t *testing.T) {
	selectionEnv(t, `
agents:
  claude: {binary: claude}
profiles:
  work:
    agent: claude
    capabilities: [network, ssh]
default_profile: work
`)
	root := runSelectionJSON(t, statusCmd())
	if got := root["network"]; got != "unrestricted" {
		t.Errorf("network = %v, want unrestricted from the network capability", got)
	}
	which := runSelectionJSON(t, whichCmd())
	sb, _ := which["sandbox"].(map[string]any)
	active, _ := sb["active"].([]any)
	skipped, _ := sb["skipped"].([]any)
	if got, want := root["sandbox_guard_count"], float64(len(active)+len(skipped)); got != want {
		t.Errorf("status guard count = %v, which shows %v guards (active and skipped)", got, want)
	}

	repo, _ := os.Getwd()
	sub := filepath.Join(repo, "pkg", "deep")
	if err := os.MkdirAll(sub, 0o750); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)
	inSub := runSelectionJSON(t, statusCmd())
	for _, k := range []string{"network", "sandbox_guard_count", "sandbox_tier"} {
		if inSub[k] != root[k] {
			t.Errorf("%s from a subdirectory = %v, from the root = %v", k, inSub[k], root[k])
		}
	}
}
