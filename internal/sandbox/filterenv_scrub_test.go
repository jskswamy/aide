package sandbox

import (
	"strings"
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

// credentialKeyModule declares credential names as agent env keys, the
// only way a credential could survive filterEnv's allow-list.
type credentialKeyModule struct{}

func (credentialKeyModule) Name() string { return "credential-keys" }
func (credentialKeyModule) Rules(*seatbelt.Context) seatbelt.GuardResult {
	return seatbelt.GuardResult{}
}
func (credentialKeyModule) AgentEnv(*seatbelt.Context) map[string]string { return nil }
func (credentialKeyModule) AgentEnvKeys(*seatbelt.Context) []string {
	return []string{"CLAUDE_CONFIG_DIR", "ANTHROPIC_API_KEY", "SOPS_AGE_KEY"}
}

func TestFilterEnv_NeverKeepsRegistryCredentials(t *testing.T) {
	env := []string{
		"PATH=/usr/bin",
		"CLAUDE_CONFIG_DIR=/run/aide/claude",
		"ANTHROPIC_API_KEY=sk-ant",
		"SOPS_AGE_KEY=AGE-SECRET-KEY-1",
	}
	filtered := filterEnv(env, Policy{AgentModule: credentialKeyModule{}})
	got := map[string]bool{}
	for _, e := range filtered {
		k, _, _ := strings.Cut(e, "=")
		got[k] = true
	}
	if !got["PATH"] || !got["CLAUDE_CONFIG_DIR"] {
		t.Errorf("filterEnv dropped essentials or the agent dir key: %v", filtered)
	}
	for _, k := range []string{"ANTHROPIC_API_KEY", "SOPS_AGE_KEY"} {
		if got[k] {
			t.Errorf("filterEnv kept credential %s", k)
		}
	}
}
