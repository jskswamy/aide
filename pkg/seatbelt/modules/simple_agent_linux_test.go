//go:build linux

package modules

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

// Landlock reads only GuardResult.Writable/Allowed, never the Seatbelt Rules,
// so every SimpleAgent-based agent needs its config dirs in Writable on Linux
// or it cannot write its own config under the sandbox.
func TestSimpleAgentLinux_ConfigDirsAreWritable(t *testing.T) {
	home := t.TempDir()
	ctx := &seatbelt.Context{HomeDir: home}

	for name, mod := range map[string]seatbelt.Module{
		"pi":       PiAgent(),
		"opencode": OpenCodeAgent(),
		"codex":    CodexAgent(),
	} {
		t.Run(name, func(t *testing.T) {
			result := mod.Rules(ctx)
			if len(result.Writable) == 0 {
				t.Fatalf("%s: Writable is empty; Landlock would grant no config dir", name)
			}
		})
	}

	want := filepath.Join(home, ".pi")
	if got := PiAgent().Rules(ctx).Writable; !slices.Contains(got, want) {
		t.Errorf("Pi Writable = %v, want it to contain %q", got, want)
	}
}

func TestSimpleAgentLinux_EnvOverrideIsWritable(t *testing.T) {
	home := t.TempDir()
	override := filepath.Join(home, "custom-pi")
	if err := os.MkdirAll(override, 0o755); err != nil {
		t.Fatal(err)
	}
	ctx := &seatbelt.Context{HomeDir: home, Env: []string{"PI_CODING_AGENT_DIR=" + override}}

	if got := PiAgent().Rules(ctx).Writable; !slices.Contains(got, override) {
		t.Errorf("Writable = %v, want it to contain the PI_CODING_AGENT_DIR override %q", got, override)
	}
}
