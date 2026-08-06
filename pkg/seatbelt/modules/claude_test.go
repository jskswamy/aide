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
	// Even with env override, non-config paths must still appear.
	ctx := &seatbelt.Context{
		HomeDir: "/home/user",
		Env:     []string{"CLAUDE_CONFIG_DIR=/custom/claude"},
	}
	result := ClaudeAgent().Rules(ctx)
	got := rulesToString(result.Rules)

	// Non-config paths that should always be present (runtime data + managed config).
	nonConfig := []string{
		".local/bin/claude",
		".cache/claude",
		".claude.json",
		".claude.lock",
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
