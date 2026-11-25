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

func TestDoctorFix_RedundantAllowInProfileBlock(t *testing.T) {
	selectionEnv(t, `
agents:
  claude: {binary: claude}
profiles:
  work:
    agent: claude
    sandbox:
      readable_extra: ["~/docs", "~/docs/api"]
default_profile: work
`)
	home, _ := os.UserHomeDir()
	if err := os.MkdirAll(filepath.Join(home, "docs", "api"), 0o750); err != nil {
		t.Fatal(err)
	}
	f := findingsByID(t)["sandbox.redundant-allow"]
	if len(f) != 1 || f[0]["subject"] != "~/docs/api" {
		t.Fatalf("sandbox.redundant-allow = %v, want one for ~/docs/api", f)
	}
	fix, _ := f[0]["fix"].(map[string]any)
	want := []string{"aide", "sandbox", "allow", "--remove", "~/docs/api", "--global", "--profile", "work"}
	if !slices.Equal(names(fix["argv"]), want) {
		t.Errorf("argv = %v, want %v", fix["argv"], want)
	}
}
