package launcher

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnvLookup(t *testing.T) {
	env := []string{"FOO=bar", "BAZ=qux", "EMPTY="}

	if got := envLookup(env, "FOO"); got != "bar" {
		t.Errorf("envLookup(FOO) = %q, want %q", got, "bar")
	}
	if got := envLookup(env, "BAZ"); got != "qux" {
		t.Errorf("envLookup(BAZ) = %q, want %q", got, "qux")
	}
	if got := envLookup(env, "MISSING"); got != "" {
		t.Errorf("envLookup(MISSING) = %q, want empty", got)
	}
	// Explicitly empty value treated as unset
	if got := envLookup(env, "EMPTY"); got != "" {
		t.Errorf("envLookup(EMPTY) = %q, want empty (treated as unset)", got)
	}
}

func TestDefaultDirs_NonExistentUnderHome(t *testing.T) {
	homeDir := t.TempDir()
	nonExistent := filepath.Join(homeDir, ".agent-config")

	dirs := defaultDirs(homeDir, nonExistent)
	if len(dirs) != 1 || dirs[0] != nonExistent {
		t.Errorf("defaultDirs should include non-existent dirs under home, got %v", dirs)
	}
}

func TestDefaultDirs_NonExistentOutsideHome(t *testing.T) {
	homeDir := t.TempDir()
	nonExistent := "/opt/some-agent-config"

	dirs := defaultDirs(homeDir, nonExistent)
	if len(dirs) != 0 {
		t.Errorf("defaultDirs should exclude non-existent dirs outside home, got %v", dirs)
	}
}

func TestDefaultDirs_ExistingDir(t *testing.T) {
	homeDir := t.TempDir()
	existing := filepath.Join(homeDir, ".agent")
	if err := os.Mkdir(existing, 0o755); err != nil {
		t.Fatal(err)
	}

	dirs := defaultDirs(homeDir, existing)
	if len(dirs) != 1 || dirs[0] != existing {
		t.Errorf("defaultDirs should include existing dir, got %v", dirs)
	}
}

// --- Claude ---

func TestClaudeConfigDirs_EnvOverride(t *testing.T) {
	env := []string{"CLAUDE_CONFIG_DIR=/custom/path"}
	dirs := claudeConfigDirs(env, "/home/user")
	if len(dirs) != 1 || dirs[0] != "/custom/path" {
		t.Errorf("expected [/custom/path], got %v", dirs)
	}
}

func TestClaudeConfigDirs_DefaultFallback(t *testing.T) {
	homeDir := t.TempDir()
	claudeDir := filepath.Join(homeDir, ".claude")
	if err := os.Mkdir(claudeDir, 0o755); err != nil {
		t.Fatal(err)
	}

	dirs := claudeConfigDirs(nil, homeDir)
	found := false
	for _, d := range dirs {
		if d == claudeDir {
			found = true
		}
	}
	if !found {
		t.Errorf("expected %s in dirs, got %v", claudeDir, dirs)
	}
}

func TestClaudeConfigDirs_NoExistingDirs(t *testing.T) {
	homeDir := t.TempDir()
	// No dirs created — should still return defaults under home (first-run support)
	dirs := claudeConfigDirs(nil, homeDir)
	if len(dirs) == 0 {
		t.Error("expected non-empty dirs for first-run support")
	}
	for _, d := range dirs {
		if !filepath.HasPrefix(d, homeDir) {
			t.Errorf("expected dir under %s, got %s", homeDir, d)
		}
	}
}

func TestClaudeConfigDirs_EmptyEnvVar(t *testing.T) {
	env := []string{"CLAUDE_CONFIG_DIR="}
	homeDir := t.TempDir()
	dirs := claudeConfigDirs(env, homeDir)
	// Empty env var treated as unset — should fall through to defaults
	if len(dirs) == 0 {
		t.Error("expected default dirs when CLAUDE_CONFIG_DIR is empty")
	}
	for _, d := range dirs {
		if d == "" {
			t.Error("should not return empty string as dir")
		}
	}
}

// --- Codex ---

func TestCodexConfigDirs_EnvOverride(t *testing.T) {
	env := []string{"CODEX_HOME=/custom"}
	dirs := codexConfigDirs(env, "/home/user")
	if len(dirs) != 1 || dirs[0] != "/custom" {
		t.Errorf("expected [/custom], got %v", dirs)
	}
}

func TestCodexConfigDirs_Default(t *testing.T) {
	homeDir := t.TempDir()
	dirs := codexConfigDirs(nil, homeDir)
	expected := filepath.Join(homeDir, ".codex")
	if len(dirs) != 1 || dirs[0] != expected {
		t.Errorf("expected [%s], got %v", expected, dirs)
	}
}

// --- Aider ---

func TestAiderConfigDirs(t *testing.T) {
	homeDir := t.TempDir()
	dirs := aiderConfigDirs(nil, homeDir)
	expected := filepath.Join(homeDir, ".aider")
	if len(dirs) != 1 || dirs[0] != expected {
		t.Errorf("expected [%s], got %v", expected, dirs)
	}
}

// --- Goose ---

func TestGooseConfigDirs_EnvOverride(t *testing.T) {
	env := []string{"GOOSE_PATH_ROOT=/custom"}
	dirs := gooseConfigDirs(env, "/home/user")
	if len(dirs) != 1 || dirs[0] != "/custom" {
		t.Errorf("expected [/custom], got %v", dirs)
	}
}

func TestGooseConfigDirs_Defaults(t *testing.T) {
	homeDir := t.TempDir()
	dirs := gooseConfigDirs(nil, homeDir)
	if len(dirs) != 3 {
		t.Errorf("expected 3 dirs, got %d: %v", len(dirs), dirs)
	}
}

// --- Amp ---

func TestAmpConfigDirs_EnvOverride(t *testing.T) {
	env := []string{"AMP_HOME=/custom"}
	dirs := ampConfigDirs(env, "/home/user")
	if len(dirs) != 1 || dirs[0] != "/custom" {
		t.Errorf("expected [/custom], got %v", dirs)
	}
}

func TestAmpConfigDirs_Defaults(t *testing.T) {
	homeDir := t.TempDir()
	dirs := ampConfigDirs(nil, homeDir)
	if len(dirs) != 2 {
		t.Errorf("expected 2 dirs, got %d: %v", len(dirs), dirs)
	}
}

// --- ResolveAgentConfigDirs ---

func TestResolveAgentConfigDirs_UnknownAgent(t *testing.T) {
	dirs := ResolveAgentConfigDirs("vim", nil, "/home/user")
	if dirs != nil {
		t.Errorf("expected nil for unknown agent, got %v", dirs)
	}
}

func TestResolveAgentConfigDirs_PathBasename(t *testing.T) {
	env := []string{"CLAUDE_CONFIG_DIR=/custom/claude"}
	dirs := ResolveAgentConfigDirs("/usr/local/bin/claude", env, "/home/user")
	if len(dirs) != 1 || dirs[0] != "/custom/claude" {
		t.Errorf("expected resolver found by basename, got %v", dirs)
	}
}
