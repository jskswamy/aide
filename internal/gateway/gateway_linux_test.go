//go:build linux

package gateway

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jskswamy/aide/internal/config"
)

func TestHarnessConfigDir_LinuxSandboxRedirect(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	if err := os.Unsetenv("CLAUDE_CONFIG_DIR"); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()

	// Unisolated sandboxed context resolves Claude's Linux sandbox redirect dir.
	got, err := HarnessConfigDir(config.Context{}, "claude", home)
	if err != nil {
		t.Fatalf("HarnessConfigDir() error = %v", err)
	}
	wantRedirect := filepath.Join(home, ".config", "aide", "claude")
	if got != wantRedirect {
		t.Errorf("sandboxed HarnessConfigDir(claude) = %q, want %q", got, wantRedirect)
	}

	// Unisolated context with sandbox disabled falls back to ~/.claude.
	unsandboxed := config.Context{Sandbox: &config.SandboxRef{Disabled: true}}
	got, err = HarnessConfigDir(unsandboxed, "claude", home)
	if err != nil {
		t.Fatalf("HarnessConfigDir(unsandboxed) error = %v", err)
	}
	wantDefault := filepath.Join(home, ".claude")
	if got != wantDefault {
		t.Errorf("unsandboxed HarnessConfigDir(claude) = %q, want %q", got, wantDefault)
	}
}
