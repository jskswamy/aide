package sandbox

import (
	"strings"
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt"
	"github.com/jskswamy/aide/pkg/seatbelt/modules"
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

func TestFilterEnv_PreservesProfileAgentConfigDirKeys(t *testing.T) {
	for name, tc := range map[string]struct {
		module func() seatbelt.Module
		envKey string
		dir    string
	}{
		"opencode": {modules.OpenCodeAgent, "OPENCODE_CONFIG_DIR", "/home/user/.opencode-work"},
		"pi":       {modules.PiAgent, "PI_CODING_AGENT_DIR", "/home/user/.pi-work"},
		"codex":    {modules.CodexAgent, "CODEX_HOME", "/home/user/.codex-work"},
		"claude":   {modules.ClaudeAgent, "CLAUDE_CONFIG_DIR", "/home/user/.claude-work"},
	} {
		t.Run(name, func(t *testing.T) {
			env := []string{
				"PATH=/usr/bin",
				tc.envKey + "=" + tc.dir,
				"UNRELATED_VAR=drop-me",
			}
			filtered := filterEnv(env, Policy{AgentModule: tc.module()})
			got := map[string]string{}
			for _, e := range filtered {
				k, v, _ := strings.Cut(e, "=")
				got[k] = v
			}
			if got[tc.envKey] != tc.dir {
				t.Errorf("%s = %q, want %q (filtered=%v)", tc.envKey, got[tc.envKey], tc.dir, filtered)
			}
			if _, ok := got["UNRELATED_VAR"]; ok {
				t.Errorf("UNRELATED_VAR kept under CleanEnv: %v", filtered)
			}
		})
	}
}
