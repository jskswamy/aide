package launcher

import (
	"path/filepath"
	"testing"
)

// An agents: alias whose binary is a profile-capable harness gets that
// harness's profile support: the profile dir is the harness's, not one
// named after the alias.
func TestLauncher_ProfileWithAgentAliasUsesHarnessDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	for name, tc := range map[string]struct {
		binary, key, dir string
	}{
		"claude": {"/opt/bin/claude", "CLAUDE_CONFIG_DIR", ".claude-work"},
		"codex":  {"codex", "CODEX_HOME", ".codex-work"},
		"pi":     {"pi", "PI_CODING_AGENT_DIR", ".pi-work"},
	} {
		t.Run(name, func(t *testing.T) {
			env, _ := launchCapturingEnvAndStderr(t, `
agents:
  work-agent:
    binary: `+tc.binary+`
profiles:
  work:
    agent: work-agent
    config_dir_name: work
default_profile: work
`, "")
			if v, _ := envValue(env, tc.key); v != filepath.Join(home, tc.dir) {
				t.Errorf("%s = %q, want %q", tc.key, v, filepath.Join(home, tc.dir))
			}
		})
	}
}
