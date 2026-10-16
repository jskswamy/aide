package modules

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

// rulesToString concatenates Rule.String() output for substring matching.
func rulesToString(rules []seatbelt.Rule) string {
	var b strings.Builder
	for _, r := range rules {
		b.WriteString(r.String())
		b.WriteByte('\n')
	}
	return b.String()
}

func TestClaudeAgent_EnvOverride(t *testing.T) {
	ctx := &seatbelt.Context{
		HomeDir: "/home/user",
		Env:     []string{"CLAUDE_CONFIG_DIR=/custom/claude"},
	}
	result := ClaudeAgent().Rules(ctx)
	got := rulesToString(result.Rules)

	// Config dir rules should reference /custom/claude only.
	if !strings.Contains(got, `/custom/claude`) {
		t.Errorf("expected /custom/claude in rules, got:\n%s", got)
	}
	// Default config dirs should NOT appear.
	if strings.Contains(got, `(subpath "/home/user/.claude")`) {
		t.Error("default .claude dir should not appear when env override is set")
	}
	if strings.Contains(got, `(subpath "/home/user/.config/claude")`) {
		t.Error("default .config/claude dir should not appear when env override is set")
	}
}

func TestClaudeAgent_DefaultConfigDirs(t *testing.T) {
	ctx := &seatbelt.Context{
		HomeDir: "/home/user",
	}
	result := ClaudeAgent().Rules(ctx)
	got := rulesToString(result.Rules)

	// Default config dirs (affected by CLAUDE_CONFIG_DIR).
	configDirs := []string{
		filepath.Join("/home/user", ".claude"),
		filepath.Join("/home/user", ".config", "claude"),
		filepath.Join("/home/user", "Library", "Application Support", "Claude"),
	}
	for _, d := range configDirs {
		if !strings.Contains(got, d) {
			t.Errorf("expected default config dir %s in rules", d)
		}
	}
}

func TestClaudeAgent_NonConfigPathsAlwaysPresent(t *testing.T) {
	// Even with env override, install and managed-config paths must
	// still appear: they hold no login state.
	ctx := &seatbelt.Context{
		HomeDir: "/home/user",
		Env:     []string{"CLAUDE_CONFIG_DIR=/custom/claude"},
	}
	result := ClaudeAgent().Rules(ctx)
	got := rulesToString(result.Rules)

	nonConfig := []string{
		".local/bin/claude",
		".cache/claude",
		".local/state/claude",
		".local/share/claude",
		".mcp.json",
		"Library/Application Support/Claude/claude_desktop_config.json",
		"Library/Application Support/ClaudeCode",
	}
	for _, p := range nonConfig {
		if !strings.Contains(got, p) {
			t.Errorf("non-config path %q should always be present in rules", p)
		}
	}
}

func TestClaudeAgent_Name(t *testing.T) {
	name := ClaudeAgent().Name()
	if name != "Claude Agent" {
		t.Errorf("expected 'Claude Agent', got %q", name)
	}
}

// With CLAUDE_CONFIG_DIR set, Claude keeps .claude.json (account and
// project state) inside that dir, so a profile launch must not reach
// the global ~/.claude.json, ~/.claude or another profile's dir. The
// profile's own refresh lock (<dir>.lock, a sibling of the dir) is
// granted instead of ~/.claude.lock.
func TestClaudeAgent_ProfileDirReplacesGlobalAccountState(t *testing.T) {
	ctx := &seatbelt.Context{
		HomeDir: "/home/user",
		Env:     []string{"CLAUDE_CONFIG_DIR=/home/user/.claude-work"},
	}
	got := rulesToString(ClaudeAgent().Rules(ctx).Rules)

	for _, forbidden := range []string{
		`"/home/user/.claude.json`,
		`"/home/user/.claude.lock"`,
		`"/home/user/.claude"`,
		`"/home/user/.claude-personal`,
		`.config/anthropic`,
	} {
		if strings.Contains(got, forbidden) {
			t.Errorf("profile launch grants %s:\n%s", forbidden, got)
		}
	}
	for _, want := range []string{
		`(subpath "/home/user/.claude-work")`,
		`(literal "/home/user/.claude-work.lock")`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("profile launch is missing %s:\n%s", want, got)
		}
	}
}

func TestClaudeAgent_TildeProfileDirLockIsAbsolute(t *testing.T) {
	ctx := &seatbelt.Context{
		HomeDir: "/home/user",
		Env:     []string{"CLAUDE_CONFIG_DIR=~/.claude-work"},
	}
	got := rulesToString(ClaudeAgent().Rules(ctx).Rules)
	if !strings.Contains(got, `(literal "/home/user/.claude-work.lock")`) {
		t.Errorf("lock grant not tilde-expanded:\n%s", got)
	}
}

// Without CLAUDE_CONFIG_DIR, Claude uses ~/.claude and ~/.claude.json;
// both stay granted.
func TestClaudeAgent_DefaultKeepsGlobalAccountState(t *testing.T) {
	got := rulesToString(ClaudeAgent().Rules(&seatbelt.Context{HomeDir: "/home/user"}).Rules)
	for _, want := range []string{
		`(prefix "/home/user/.claude.json")`,
		`(literal "/home/user/.claude.lock")`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("default launch is missing %s:\n%s", want, got)
		}
	}
	if strings.Contains(got, ".config/anthropic") {
		t.Errorf("default launch grants ~/.config/anthropic:\n%s", got)
	}
}
