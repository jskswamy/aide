package modules

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

// With a profile dir selected, the Pi and Codex modules' Seatbelt rules
// (the macOS backend) grant that dir only: not the default dir, Pi's
// ~/.pi parent, or another profile's dir, even when they exist.
func TestProfileDirRulesGrantOnlySelectedDir(t *testing.T) {
	for name, tc := range map[string]struct {
		module func() seatbelt.Module
		envKey string
		work   string
		others []string
	}{
		"pi":       {PiAgent, "PI_CODING_AGENT_DIR", ".pi-work", []string{".pi", ".pi/agent", ".pi-personal"}},
		"codex":    {CodexAgent, "CODEX_HOME", ".codex-work", []string{".codex", ".codex-personal"}},
		"opencode": {OpenCodeAgent, "OPENCODE_CONFIG_DIR", ".opencode-work", []string{".config/opencode", ".opencode-personal"}},
	} {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			for _, d := range append([]string{tc.work}, tc.others...) {
				if err := os.MkdirAll(filepath.Join(home, d), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			work := filepath.Join(home, tc.work)
			ctx := &seatbelt.Context{HomeDir: home, Env: []string{tc.envKey + "=" + work}}

			got := rulesToString(tc.module().Rules(ctx).Rules)

			if !strings.Contains(got, `(subpath "`+work+`")`) {
				t.Errorf("rules do not grant the profile dir %s:\n%s", work, got)
			}
			for _, rel := range tc.others {
				other := filepath.Join(home, rel)
				if strings.Contains(got, `"`+other+`"`) {
					t.Errorf("rules grant %s:\n%s", other, got)
				}
			}
		})
	}
}

// OPENCODE_CONFIG_DIR moves only the config dir; the XDG data dir
// (log, db, auth) must stay writable or OpenCode dies with EPERM
// opening ~/.local/share/opencode/log/opencode.log.
func TestOpenCodeConfigDirOverrideKeepsDataDirs(t *testing.T) {
	home := t.TempDir()
	work := filepath.Join(home, ".opencode-work")
	for _, d := range []string{work, ".local/share/opencode", ".local/state/opencode", ".cache/opencode"} {
		if err := os.MkdirAll(filepath.Join(home, d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	ctx := &seatbelt.Context{HomeDir: home, Env: []string{"OPENCODE_CONFIG_DIR=" + work}}

	got := rulesToString(OpenCodeAgent().Rules(ctx).Rules)

	for _, rel := range []string{".opencode-work", ".local/share/opencode", ".local/state/opencode", ".cache/opencode"} {
		if !strings.Contains(got, `(subpath "`+filepath.Join(home, rel)+`")`) {
			t.Errorf("rules do not grant %s:\n%s", rel, got)
		}
	}
}
