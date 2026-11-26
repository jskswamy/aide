package main

import (
	"strings"
	"testing"
)

func TestDoctor_RetiredGitRemoteIsReported(t *testing.T) {
	selectionEnv(t, `
agents:
  claude: {binary: claude}
profiles:
  work:
    agent: claude
    capabilities: [git-remote]
    sandbox:
      guards_extra: [git-remote]
default_profile: work
`)
	f := findingsByID(t)["capability.retired"]
	if len(f) != 2 {
		t.Fatalf("capability.retired = %v, want one for the capability and one for the guard", f)
	}
	for _, x := range f {
		fix, _ := x["fix"].(map[string]any)
		if x["severity"] != "tip" || len(names(fix["steps"])) == 0 {
			t.Errorf("finding = %v, want a tip with steps", x)
		}
	}
}

func TestDoctor_RetiredGuardInNamedSandboxGivesEditSteps(t *testing.T) {
	selectionEnv(t, `
agents:
  claude: {binary: claude}
sandboxes:
  shared:
    guards_extra: [git-remote]
profiles:
  work: {agent: claude, sandbox: shared}
default_profile: work
`)
	for _, f := range findingsByID(t)["capability.retired"] {
		if f["subject"] != "guard:git-remote" {
			continue
		}
		fix, _ := f["fix"].(map[string]any)
		steps := strings.Join(names(fix["steps"]), " ")
		if !strings.Contains(steps, "shared") || strings.Contains(steps, "unguard") {
			t.Errorf("steps = %q, want an edit of the named sandbox shared", steps)
		}
		return
	}
	t.Fatal("no retired-guard finding")
}
