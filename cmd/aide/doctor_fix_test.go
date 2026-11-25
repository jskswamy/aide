package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/jskswamy/aide/internal/trust"
)

func suggestedGo(t *testing.T) map[string]any {
	t.Helper()
	for _, f := range findingsByID(t)["capability.suggested"] {
		if f["subject"] == "go" {
			return f
		}
	}
	t.Fatal("no capability.suggested finding for go")
	return nil
}

func TestDoctorFix_SuggestsCapabilityFromMarker(t *testing.T) {
	selectionEnv(t, "agents:\n  claude: {binary: claude}\nprofiles:\n  work: {agent: claude}\ndefault_profile: work\n")
	repo, _ := os.Getwd()
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f := suggestedGo(t)
	fix, _ := f["fix"].(map[string]any)
	if f["widens_access"] != true || len(names(fix["argv"])) != 0 || !slices.Contains(names(fix["steps"]), "Run: aide trust") {
		t.Errorf("without a trusted .aide.yaml: finding = %v, want steps ending in aide trust", f)
	}

	body := "capabilities: [nix]\n"
	path := filepath.Join(repo, ".aide.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := trust.DefaultStore().Trust(path, []byte(body)); err != nil {
		t.Fatal(err)
	}
	f = suggestedGo(t)
	fix, _ = f["fix"].(map[string]any)
	if !slices.Equal(names(fix["argv"]), []string{"aide", "cap", "enable", "go"}) {
		t.Errorf("with a trusted .aide.yaml: argv = %v, want aide cap enable go", fix["argv"])
	}
}
