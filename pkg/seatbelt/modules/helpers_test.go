package modules

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

func TestResolveConfigDirs_EnvOverride(t *testing.T) {
	ctx := &seatbelt.Context{
		HomeDir: "/home/user",
		Env:     []string{"CLAUDE_CONFIG_DIR=/custom/claude"},
	}
	dirs := resolveConfigDirs(ctx, "CLAUDE_CONFIG_DIR", []string{
		"/home/user/.claude",
		"/home/user/.config/claude",
	})
	if len(dirs) != 1 || dirs[0] != "/custom/claude" {
		t.Errorf("expected [/custom/claude], got %v", dirs)
	}
}

// TestResolveConfigDirs_TildeExpansion verifies env values like
// CLAUDE_CONFIG_DIR=~/.claude-prod are expanded to absolute paths
// before being returned for sandbox rule emission. Without expansion,
// Seatbelt subpath rules containing "~/..." literal strings never
// match the absolute paths the agent actually opens.
func TestResolveConfigDirs_TildeExpansion(t *testing.T) {
	ctx := &seatbelt.Context{
		HomeDir: "/home/user",
		Env:     []string{"CLAUDE_CONFIG_DIR=~/.claude-prod"},
	}
	dirs := resolveConfigDirs(ctx, "CLAUDE_CONFIG_DIR", nil)
	if len(dirs) != 1 || dirs[0] != "/home/user/.claude-prod" {
		t.Errorf("expected [/home/user/.claude-prod], got %v", dirs)
	}
}

func TestResolveConfigDirs_EmptyEnv(t *testing.T) {
	ctx := &seatbelt.Context{
		HomeDir: "/home/user",
		Env:     []string{"CLAUDE_CONFIG_DIR="},
	}
	// Empty env var is treated as unset — falls through to defaults.
	// Only candidates under homeDir pass ExistsOrUnderHome.
	dirs := resolveConfigDirs(ctx, "CLAUDE_CONFIG_DIR", []string{
		"/home/user/.claude",
		"/home/user/.config/claude",
	})
	if len(dirs) != 2 {
		t.Errorf("expected 2 default dirs, got %v", dirs)
	}
}

func TestResolveConfigDirs_NoEnvKey(t *testing.T) {
	ctx := &seatbelt.Context{
		HomeDir: "/home/user",
	}
	dirs := resolveConfigDirs(ctx, "", []string{
		"/home/user/.claude",
		"/somewhere/else",
	})
	// /somewhere/else doesn't exist and isn't under home
	if len(dirs) != 1 || dirs[0] != "/home/user/.claude" {
		t.Errorf("expected [/home/user/.claude], got %v", dirs)
	}
}

func TestResolveConfigDirs_ExistingOutsideHome(t *testing.T) {
	// Create a temp dir that exists on disk but is outside homeDir.
	tmpDir := t.TempDir()
	ctx := &seatbelt.Context{
		HomeDir: "/home/user",
	}
	dirs := resolveConfigDirs(ctx, "", []string{tmpDir})
	if len(dirs) != 1 || dirs[0] != tmpDir {
		t.Errorf("expected existing dir %s to be included, got %v", tmpDir, dirs)
	}
}

func TestResolveConfigDirs_NonExistentOutsideHome(t *testing.T) {
	ctx := &seatbelt.Context{
		HomeDir: "/home/user",
	}
	dirs := resolveConfigDirs(ctx, "", []string{
		filepath.Join(os.TempDir(), "nonexistent-aide-test-path"),
	})
	if len(dirs) != 0 {
		t.Errorf("expected no dirs for non-existent path outside home, got %v", dirs)
	}
}

func TestResolveConfigDirsAdditive_SafeOverride(t *testing.T) {
	ctx := &seatbelt.Context{
		HomeDir: "/home/user",
		Env:     []string{"CURSOR_CONFIG_DIR=/home/user/my-cursor"},
	}
	defaults := []string{"/home/user/.cursor"}

	dirs := resolveConfigDirsAdditive(ctx, "CURSOR_CONFIG_DIR", "/home/user/.config/cursor", defaults)

	if len(dirs) != 1 || dirs[0] != "/home/user/my-cursor" {
		t.Errorf("expected [/home/user/my-cursor], got %v", dirs)
	}
}

func TestResolveConfigDirsAdditive_RejectsSensitiveOverride(t *testing.T) {
	ctx := &seatbelt.Context{
		HomeDir: "/home/user",
		Env:     []string{"CURSOR_CONFIG_DIR=/home/user/.ssh"},
	}
	defaults := []string{"/home/user/.cursor"}
	xdg := "/home/user/.config/cursor"

	dirs := resolveConfigDirsAdditive(ctx, "CURSOR_CONFIG_DIR", xdg, defaults)

	for _, d := range dirs {
		if strings.Contains(d, ".ssh") {
			t.Errorf("resolveConfigDirsAdditive must not include .ssh path; got %v", dirs)
		}
	}
	if len(dirs) != 2 {
		t.Errorf("expected 2 fallback dirs (default + xdg), got %v", dirs)
	}
}

func TestResolveConfigDirsAdditive_RejectsSensitiveXdgCandidate(t *testing.T) {
	ctx := &seatbelt.Context{
		HomeDir: "/home/user",
	}
	dirs := resolveConfigDirsAdditive(ctx, "CURSOR_CONFIG_DIR", "/home/user/.ssh/cursor", []string{"/home/user/.cursor"})

	for _, d := range dirs {
		if strings.Contains(d, ".ssh") {
			t.Errorf("resolveConfigDirsAdditive must not include .ssh path; got %v", dirs)
		}
	}
	if len(dirs) != 1 || dirs[0] != "/home/user/.cursor" {
		t.Errorf("expected only default dir when xdgCandidate is unsafe, got %v", dirs)
	}
}

func TestResolveConfigDirsAdditive_RejectsOutsideHome(t *testing.T) {
	ctx := &seatbelt.Context{
		HomeDir: "/home/user",
		Env:     []string{"CURSOR_CONFIG_DIR=/etc/cursor"},
	}
	defaults := []string{"/home/user/.cursor"}
	xdg := "/home/user/.config/cursor"

	dirs := resolveConfigDirsAdditive(ctx, "CURSOR_CONFIG_DIR", xdg, defaults)

	if len(dirs) != 2 {
		t.Errorf("expected 2 fallback dirs, got %v", dirs)
	}
	for _, d := range dirs {
		if d == "/etc/cursor" {
			t.Errorf("override outside $HOME must be rejected; got %v", dirs)
		}
	}
}

func TestResolveConfigDirsAdditive_NoOverride(t *testing.T) {
	ctx := &seatbelt.Context{HomeDir: "/home/user"}
	defaults := []string{"/home/user/.cursor"}
	xdg := "/home/user/.config/cursor"

	dirs := resolveConfigDirsAdditive(ctx, "CURSOR_CONFIG_DIR", xdg, defaults)

	if len(dirs) != 2 || dirs[0] != "/home/user/.cursor" || dirs[1] != xdg {
		t.Errorf("expected [default, xdg], got %v", dirs)
	}
}

func TestConfigDirRules_Empty(t *testing.T) {
	rules := configDirRules("Claude", nil)
	if rules != nil {
		t.Errorf("expected nil for empty dirs, got %v", rules)
	}
}

func TestConfigDirRules_Single(t *testing.T) {
	rules := configDirRules("Claude", []string{"/home/user/.claude"})
	if len(rules) != 2 {
		t.Fatalf("expected 2 rules (section + grant), got %d", len(rules))
	}
	// First rule is section header
	if !strings.Contains(rules[0].String(), "Claude config") {
		t.Errorf("expected section header with 'Claude config', got %q", rules[0].String())
	}
	// Second rule is the grant
	got := rules[1].String()
	want := fmt.Sprintf(`(allow file-read* file-write* (subpath %q))`, "/home/user/.claude")
	if !strings.Contains(got, want) {
		t.Errorf("expected grant rule containing %q, got %q", want, got)
	}
	if rules[1].Intent() != seatbelt.Allow {
		t.Errorf("expected Allow intent, got %d", rules[1].Intent())
	}
}

func TestConfigDirRules_Multiple(t *testing.T) {
	dirs := []string{"/home/user/.claude", "/home/user/.config/claude"}
	rules := configDirRules("Claude", dirs)
	// 1 section + 2 grant rules
	if len(rules) != 3 {
		t.Fatalf("expected 3 rules, got %d", len(rules))
	}
	for i, dir := range dirs {
		got := rules[i+1].String()
		want := fmt.Sprintf(`(subpath %q)`, dir)
		if !strings.Contains(got, want) {
			t.Errorf("rule[%d]: expected %q in %q", i+1, want, got)
		}
	}
}
