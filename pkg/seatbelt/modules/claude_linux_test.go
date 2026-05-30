//go:build linux

package modules

import (
	"os"
	"path/filepath"
	"slices"
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

// TestClaudeAgent_LinuxWritablePathsInRules_FollowsSkillsSymlink mirrors the
// Cursor symlink-following test: a user-managed symlink at
// CLAUDE_CONFIG_DIR/skills pointing at a dotfiles repo must result in the
// resolved target being added to Writable. Without it Landlock denies access
// at the resolved inode and Claude can't load skills.
func TestClaudeAgent_LinuxWritablePathsInRules_FollowsSkillsSymlink(t *testing.T) {
	home := t.TempDir()
	configDir := filepath.Join(home, ".config", "aide", "claude")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatalf("setup config dir: %v", err)
	}
	skillsTarget := filepath.Join(home, "dotfiles", "claude-skills")
	if err := os.MkdirAll(skillsTarget, 0o700); err != nil {
		t.Fatalf("setup skills target: %v", err)
	}
	if err := os.Symlink(skillsTarget, filepath.Join(configDir, "skills")); err != nil {
		t.Fatalf("setup symlink: %v", err)
	}

	mod := ClaudeAgent()
	ctx := &seatbelt.Context{HomeDir: home, GOOS: "linux"}
	result := mod.Rules(ctx)

	if !slices.Contains(result.Writable, configDir) {
		t.Errorf("Writable must contain config dir %q; got %v", configDir, result.Writable)
	}
	if !slices.Contains(result.Writable, skillsTarget) {
		t.Errorf("Writable must contain symlink target %q so Landlock allows skill reads/writes; got %v",
			skillsTarget, result.Writable)
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

func TestClaudeAgent_AgentEnvKeys_ReturnsCLAUDE_CONFIG_DIR(t *testing.T) {
	mod := ClaudeAgent()
	keyProvider, ok := mod.(seatbelt.EnvKeyProvider)
	if !ok {
		t.Fatal("ClaudeAgent must implement seatbelt.EnvKeyProvider for side-effect-free key discovery")
	}

	// Arrange: context is irrelevant — AgentEnvKeys must return the same keys
	// regardless of whether CLAUDE_CONFIG_DIR is set, and must not call MkdirAll.
	for _, ctx := range []*seatbelt.Context{
		nil,
		{HomeDir: ""},
		{HomeDir: "/home/user", GOOS: "linux"},
		{HomeDir: "/home/user", GOOS: "linux", Env: []string{"CLAUDE_CONFIG_DIR=/custom"}},
	} {
		// Act
		keys := keyProvider.AgentEnvKeys(ctx)

		// Assert
		if len(keys) != 1 || keys[0] != "CLAUDE_CONFIG_DIR" {
			t.Errorf("AgentEnvKeys(ctx=%v) = %v, want [CLAUDE_CONFIG_DIR]", ctx, keys)
		}
	}
}

func TestClaudeAgent_AgentEnv_MkdirAllFailure_ReturnsNil(t *testing.T) {
	// Arrange: use a file as the parent so MkdirAll fails.
	parentFile := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(parentFile, []byte("x"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	// The aide default dir is <homeDir>/.config/aide/claude. By making homeDir
	// point at a file-as-dir, MkdirAll will fail trying to descend into it.
	mod := ClaudeAgent()
	provider, ok := mod.(seatbelt.EnvProvider)
	if !ok {
		t.Fatal("ClaudeAgent must implement seatbelt.EnvProvider")
	}
	ctx := &seatbelt.Context{HomeDir: parentFile, GOOS: "linux"}

	// Act
	got := provider.AgentEnv(ctx)

	// Assert: failure must return nil, not inject a broken path.
	if got != nil {
		t.Errorf("AgentEnv must return nil when MkdirAll fails; got %v", got)
	}
}

func TestClaudeAgent_AugmentLinuxPaths_NilOrEmptyContext(t *testing.T) {
	result := &seatbelt.GuardResult{}

	// Arrange + Act: nil and empty-HomeDir context must be no-ops.
	augmentLinuxPaths(nil, result)
	augmentLinuxPaths(&seatbelt.Context{HomeDir: ""}, result)

	// Assert
	if len(result.Writable) != 0 {
		t.Errorf("augmentLinuxPaths with nil/empty context must not append paths; got %v", result.Writable)
	}
}
