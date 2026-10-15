//go:build linux

package main

import (
	"path/filepath"
	"testing"

	"github.com/jskswamy/aide/internal/config"
)

// TestResolveProvisionContext_ClaudeSandboxRedirect pins that sync
// targets the config dir a sandboxed Claude actually reads on Linux
// (~/.config/aide/claude), not Claude's default ~/.claude*.
func TestResolveProvisionContext_ClaudeSandboxRedirect(t *testing.T) {
	unsetEnv(t, "CLAUDE_CONFIG_DIR")
	home := t.TempDir()
	cfg := &config.Config{}
	pCtx, err := resolveProvisionContext(cfg, "c", config.Context{Agent: "claude"}, home, home)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".config", "aide", "claude")
	if got := pCtx.Env["CLAUDE_CONFIG_DIR"]; got != want {
		t.Errorf("CLAUDE_CONFIG_DIR = %q, want %q", got, want)
	}
}

func TestResolveProvisionContext_SandboxDisabledKeepsDefault(t *testing.T) {
	unsetEnv(t, "CLAUDE_CONFIG_DIR")
	home := t.TempDir()
	cfg := &config.Config{}
	ctx := config.Context{Agent: "claude", Sandbox: &config.SandboxRef{Disabled: true}}
	pCtx, err := resolveProvisionContext(cfg, "c", ctx, home, home)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := pCtx.Env["CLAUDE_CONFIG_DIR"]; ok {
		t.Errorf("unsandboxed launch uses Claude's default dir; got CLAUDE_CONFIG_DIR=%q", got)
	}
}

func TestResolveProvisionContext_ProfileWins(t *testing.T) {
	unsetEnv(t, "CLAUDE_CONFIG_DIR")
	home := t.TempDir()
	cfg := &config.Config{}
	ctx := config.Context{Agent: "claude", Profile: "work"}
	pCtx, err := resolveProvisionContext(cfg, "c", ctx, home, home)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".claude-work")
	if got := pCtx.Env["CLAUDE_CONFIG_DIR"]; got != want {
		t.Errorf("CLAUDE_CONFIG_DIR = %q, want profile dir %q", got, want)
	}
}
