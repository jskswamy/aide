package main

import (
	"strings"
	"testing"
)

func TestDoctorWarn_TiedProfilesAndDefaultSandbox(t *testing.T) {
	selectionEnv(t, `
agents:
  claude: {binary: claude}
profiles:
  alpha: {agent: claude, match: [{path: "~/*"}]}
  beta: {agent: claude, match: [{path: "~/*"}]}
`)
	f := findingsByID(t)
	tie := f["profile.match-tie"]
	if len(tie) != 1 || tie[0]["severity"] != "warning" || tie[0]["profile"] != "alpha" {
		t.Errorf("profile.match-tie = %v, want one warning on alpha", tie)
	}
	if def := f["sandbox.default-policy"]; len(def) != 1 || def[0]["severity"] != "tip" {
		t.Errorf("sandbox.default-policy = %v, want one tip", def)
	}
}

func TestDoctorWarn_NoTieWhenProfileChosen(t *testing.T) {
	selectionEnv(t, `
agents:
  claude: {binary: claude}
profiles:
  alpha: {agent: claude, match: [{path: "~/*"}]}
  beta: {agent: claude, match: [{path: "~/*"}]}
`)
	got, _ := execDoctor(t, "--profile", "beta")
	if findingIDs(got)["profile.match-tie"] {
		t.Errorf("profile.match-tie reported although --profile chose the profile: %v", got["findings"])
	}
}

func TestDoctorWarn_MissingAbsoluteBinary(t *testing.T) {
	selectionEnv(t, "agents:\n  claude: {binary: /no/such/claude}\nprofiles:\n  work: {agent: claude}\ndefault_profile: work\n")
	if f := findingsByID(t)["agent.not-found"]; len(f) != 1 {
		t.Errorf("agent.not-found = %v, want one for an absolute path that does not exist", f)
	}
}

func TestStatus_BadSandboxRefStillReports(t *testing.T) {
	selectionEnv(t, "agents:\n  claude: {binary: claude}\nprofiles:\n  work: {agent: claude, sandbox: nope}\ndefault_profile: work\n")
	got := runSelectionJSON(t, statusCmd())
	if tier, _ := got["sandbox_tier"].(string); !strings.Contains(tier, "does not resolve") {
		t.Errorf("sandbox_tier = %q, want it to say the sandbox does not resolve", tier)
	}
}
