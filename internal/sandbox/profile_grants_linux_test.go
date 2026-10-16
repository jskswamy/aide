//go:build linux

package sandbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt"
	"github.com/jskswamy/aide/pkg/seatbelt/modules"
)

// coversPath reports whether a grant of p reaches target (p is target
// or one of its parents).
func coversPath(p, target string) bool {
	return p == target || strings.HasPrefix(target, p+string(filepath.Separator))
}

// A Pi or Codex profile launch under Landlock grants the selected
// profile dir and nothing of the harness's default dir (or its ~/.pi
// parent) or another profile's dir, with the default guards.
func TestLandlockProfileLaunchGrantsOnlySelectedDir(t *testing.T) {
	for name, tc := range map[string]struct {
		module func() seatbelt.Module
		envKey string
		work   string
		others []string
	}{
		"pi":    {modules.PiAgent, "PI_CODING_AGENT_DIR", ".pi-work", []string{".pi", ".pi/agent", ".pi-personal"}},
		"codex": {modules.CodexAgent, "CODEX_HOME", ".codex-work", []string{".codex", ".codex-personal"}},
	} {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			work := filepath.Join(home, tc.work)
			for _, d := range append([]string{tc.work}, tc.others...) {
				if err := os.MkdirAll(filepath.Join(home, d), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			policy := DefaultPolicy(Paths{ProjectRoot: t.TempDir(), RuntimeDir: t.TempDir(), TempDir: t.TempDir(), HomeDir: home},
				[]string{tc.envKey + "=" + work})
			policy.AgentModule = tc.module()

			gps := DeriveGrantedPathSet(policy)

			if !containsPath(gps.Writable, work) {
				t.Errorf("profile dir %s not writable: %v", work, gps.Writable)
			}
			for _, rel := range tc.others {
				other := filepath.Join(home, rel)
				for _, p := range append(append([]string{}, gps.Writable...), gps.Readable...) {
					if coversPath(p, other) {
						t.Errorf("%s granted through %s", other, p)
					}
				}
			}
		})
	}
}
