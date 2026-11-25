package main

import "testing"

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
