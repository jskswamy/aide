package main

import (
	"strings"
	"testing"
)

func TestDoctorTips_NetworkAndOptInGuards(t *testing.T) {
	selectionEnv(t, "agents:\n  claude: {binary: claude}\nprofiles:\n  work:\n    agent: claude\n    sandbox:\n      network: {mode: unrestricted}\ndefault_profile: work\n")
	f := findingsByID(t)
	if n := f["sandbox.network-unrestricted"]; len(n) != 1 || n[0]["severity"] != "tip" {
		t.Errorf("sandbox.network-unrestricted = %v, want one tip", n)
	}
	g := f["sandbox.opt-in-guards"]
	if len(g) != 1 {
		t.Fatalf("sandbox.opt-in-guards = %v, want one tip", g)
	}
	if msg, _ := g[0]["message"].(string); !strings.Contains(msg, "ssh") {
		t.Errorf("message = %q, want the available guards named", msg)
	}
}
