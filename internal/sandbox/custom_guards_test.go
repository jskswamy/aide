package sandbox

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/pkg/seatbelt/guards"
)

func TestPolicyFromConfig_EnforcesCustomGuards(t *testing.T) {
	t.Cleanup(func() { _ = guards.SetCustomGuards(nil) })
	home := t.TempDir()
	for _, d := range []string{".config/audit", ".config/team-vault"} {
		if err := os.MkdirAll(filepath.Join(home, d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := guards.SetCustomGuards(map[string]guards.CustomGuardConfig{
		"audit-logs": {Type: "default", Paths: []string{"~/.config/audit"}},
		"team-vault": {Type: "opt-in", Paths: []string{"~/.config/team-vault"}},
	}); err != nil {
		t.Fatal(err)
	}

	policy, _, err := PolicyFromConfig(&config.SandboxPolicy{GuardsExtra: []string{"team-vault"}}, Paths{HomeDir: home, ProjectRoot: t.TempDir()})
	if err != nil {
		t.Fatalf("PolicyFromConfig: %v", err)
	}
	if len(policy.CustomGuards) != 2 {
		t.Errorf("policy.CustomGuards = %v, want both definitions for the sandbox child", policy.CustomGuards)
	}
	var protected []string
	for _, r := range EvaluateGuards(policy) {
		protected = append(protected, r.Protected...)
	}
	for _, want := range []string{filepath.Join(home, ".config/audit"), filepath.Join(home, ".config/team-vault")} {
		if !slices.Contains(protected, want) {
			t.Errorf("protected paths %v missing %s", protected, want)
		}
	}
}
