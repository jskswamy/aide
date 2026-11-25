package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDoctorXCheck_GPUSuggestedFromImports(t *testing.T) {
	selectionEnv(t, "agents:\n  claude: {binary: claude}\nprofiles:\n  work: {agent: claude}\ndefault_profile: work\n")
	repo, _ := os.Getwd()
	if err := os.WriteFile(filepath.Join(repo, "requirements.txt"), []byte("numpy\ntorch==2.5\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var gpu map[string]any
	for _, f := range findingsByID(t)["capability.suggested"] {
		if f["subject"] == "gpu" {
			gpu = f
		}
	}
	if gpu == nil || gpu["widens_access"] != true {
		t.Fatalf("want a gpu suggestion that widens access, got %v", gpu)
	}
}

func TestDoctorXCheck_CapabilityPathDenied(t *testing.T) {
	selectionEnv(t, `
agents:
  claude: {binary: claude}
profiles:
  work:
    agent: claude
    capabilities: [go]
    sandbox:
      denied_extra: ["~/go"]
default_profile: work
`)
	home, _ := os.UserHomeDir()
	if err := os.MkdirAll(filepath.Join(home, "go"), 0o750); err != nil {
		t.Fatal(err)
	}
	f := findingsByID(t)["capability.path-denied"]
	if len(f) != 1 || f[0]["severity"] != "warning" {
		t.Errorf("capability.path-denied = %v, want one warning for go", f)
	}
}
