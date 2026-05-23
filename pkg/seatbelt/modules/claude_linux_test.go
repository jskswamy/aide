//go:build linux

package modules

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

func TestClaudeAgent_AgentEnv_InjectsDefaultWhenUserHasNoOverride(t *testing.T) {
	homeDir := t.TempDir()
	mod := ClaudeAgent()
	provider, ok := mod.(seatbelt.EnvProvider)
	if !ok {
		t.Fatal("ClaudeAgent must implement seatbelt.EnvProvider so the launcher can inject CLAUDE_CONFIG_DIR")
	}
	ctx := &seatbelt.Context{HomeDir: homeDir, GOOS: "linux"} // empty Env → no user override

	got := provider.AgentEnv(ctx)
	want := filepath.Join(homeDir, ".config", "aide", "claude")
	if got["CLAUDE_CONFIG_DIR"] != want {
		t.Errorf("AgentEnv[CLAUDE_CONFIG_DIR] = %q, want %q", got["CLAUDE_CONFIG_DIR"], want)
	}
	// The dir must be created — Landlock grants access on the path but
	// cannot create the path itself.
	if info, err := os.Stat(want); err != nil || !info.IsDir() {
		t.Errorf("expected directory %q to exist after AgentEnv; err=%v", want, err)
	}
}

func TestClaudeAgent_AgentEnv_RespectsUserOverride(t *testing.T) {
	homeDir := t.TempDir()
	userChoice := "/work/foo/.claude-state"
	mod := ClaudeAgent()
	provider, ok := mod.(seatbelt.EnvProvider)
	if !ok {
		t.Fatal("ClaudeAgent must implement seatbelt.EnvProvider")
	}
	ctx := &seatbelt.Context{
		HomeDir: homeDir,
		GOOS:    "linux",
		Env:     []string{"CLAUDE_CONFIG_DIR=" + userChoice},
	}

	got := provider.AgentEnv(ctx)
	if got != nil {
		t.Errorf("AgentEnv should return nil when user has CLAUDE_CONFIG_DIR set; got %v", got)
	}
	// aide must not have created its default dir when honouring the user's choice.
	defaultDir := filepath.Join(homeDir, ".config", "aide", "claude")
	if _, err := os.Stat(defaultDir); err == nil {
		t.Errorf("aide should not create its default dir when user has CLAUDE_CONFIG_DIR set; %q exists", defaultDir)
	}
}

func TestClaudeAgent_AgentEnv_NilContext(t *testing.T) {
	mod := ClaudeAgent()
	provider, ok := mod.(seatbelt.EnvProvider)
	if !ok {
		t.Fatal("ClaudeAgent must implement seatbelt.EnvProvider")
	}
	if got := provider.AgentEnv(nil); got != nil {
		t.Errorf("AgentEnv(nil) should return nil, got %v", got)
	}
	if got := provider.AgentEnv(&seatbelt.Context{HomeDir: ""}); got != nil {
		t.Errorf("AgentEnv(empty home) should return nil, got %v", got)
	}
}

// TestClaudeAgent_LinuxWritablePathsInRules asserts the module exposes exactly
// the redirect dir as writable — no broader $HOME paths. Sensitive siblings
// (~/.ssh, ~/.aws, etc.) are not deny-listed; they're absent from the Landlock
// allow-list, so the agent has no way to reach them.
func TestClaudeAgent_LinuxWritablePathsInRules_OnlyRedirectDir(t *testing.T) {
	homeDir := t.TempDir()
	mod := ClaudeAgent()
	ctx := &seatbelt.Context{HomeDir: homeDir, GOOS: "linux"}
	result := mod.Rules(ctx)

	wantDir := filepath.Join(homeDir, ".config", "aide", "claude")
	if len(result.Writable) != 1 || result.Writable[0] != wantDir {
		t.Errorf("Writable should be exactly [%q], got %v", wantDir, result.Writable)
	}
	// Old hardcoded paths must not appear.
	for _, p := range result.Writable {
		for _, stale := range []string{".claude", ".cache/claude", ".local/state/claude", ".local/share/claude", ".claude.json"} {
			if strings.HasSuffix(p, stale) {
				t.Errorf("legacy hardcoded path %q must not appear in Writable", p)
			}
		}
	}
}

func TestClaudeAgent_LinuxWritablePathsInRules_UserOverrideHonoured(t *testing.T) {
	homeDir := t.TempDir()
	userChoice := "/work/foo/.claude-state"
	mod := ClaudeAgent()
	ctx := &seatbelt.Context{
		HomeDir: homeDir,
		GOOS:    "linux",
		Env:     []string{"CLAUDE_CONFIG_DIR=" + userChoice},
	}
	result := mod.Rules(ctx)

	if len(result.Writable) != 1 || result.Writable[0] != userChoice {
		t.Errorf("Writable should track user's CLAUDE_CONFIG_DIR; got %v want [%q]", result.Writable, userChoice)
	}
}
