package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jskswamy/aide/internal/capability"
	"github.com/jskswamy/aide/internal/consent"
)

// which shows the variant launch would use, including a choice the user
// already saved, not the non-interactive default.
func TestWhich_ShowsSavedVariantChoice(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	selectionEnv(t, `
agents:
  claude: {binary: claude}
profiles:
  work:
    agent: claude
    capabilities: [python]
default_profile: work
`)
	repo, _ := os.Getwd()
	if err := os.WriteFile(filepath.Join(repo, "uv.lock"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	py := capability.Builtins()["python"]
	evidence := capability.DetectEvidence(os.DirFS(repo), py)
	if err := consent.DefaultStore().Grant(consent.Grant{
		ProjectRoot: repo, Capability: "python", Variants: evidence.Variants,
		Evidence: evidence, ConfirmedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}

	got := runSelectionJSON(t, whichCmd())
	caps, _ := got["capabilities"].([]any)
	for _, c := range caps {
		m, _ := c.(map[string]any)
		if m["name"] != "python" {
			continue
		}
		vs := names(m["variants"])
		if len(vs) == 0 || vs[0] != "uv" {
			t.Errorf("python variants = %v, want the saved choice uv; entry %v", vs, m)
		}
		return
	}
	t.Fatalf("python capability missing from which output: %v", got["capabilities"])
}
