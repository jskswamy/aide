//go:build linux

package launcher

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/sandbox"
)

// TestApplyAgentEnv_InjectedKeysReflectedInPolicyEnv verifies that keys added
// by applyAgentEnv (e.g. CLAUDE_CONFIG_DIR) are present in policy.Env after the
// sync that follows the call in launcher.go. Without that sync the re-exec child
// would read a stale policy.Env and resolve capability-guarded paths against the
// default value instead of the injected one, causing silent EACCES.
//
// Linux-only: claudeAgentModule.AgentEnv is build-tagged to Linux; the macOS
// stub in claude_other.go is a no-op (CLAUDE_CONFIG_DIR redirect is not wired
// through Seatbelt yet).
func TestApplyAgentEnv_InjectedKeysReflectedInPolicyEnv(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}

	policy := &sandbox.Policy{
		HomeDir:     home,
		AgentModule: ResolveAgentModule("claude"),
	}

	env := []string{"HOME=" + home}
	policy.Env = env

	env = applyAgentEnv(env, policy)
	policy.Env = env

	var injected string
	for _, kv := range policy.Env {
		if strings.HasPrefix(kv, "CLAUDE_CONFIG_DIR=") {
			injected = strings.TrimPrefix(kv, "CLAUDE_CONFIG_DIR=")
		}
	}
	if injected == "" {
		t.Fatal("policy.Env missing CLAUDE_CONFIG_DIR after applyAgentEnv sync")
	}
	if !strings.HasPrefix(injected, home) {
		t.Errorf("CLAUDE_CONFIG_DIR %q not rooted under home %q", injected, home)
	}
}

// TestSandboxAgentEnv_ClaudeRedirect pins that callers provisioning
// outside a launch (aide sync, sandbox grants) see the same
// CLAUDE_CONFIG_DIR redirect the sandboxed launch injects.
func TestSandboxAgentEnv_ClaudeRedirect(t *testing.T) {
	home := t.TempDir()
	got := SandboxAgentEnv("claude", home, []string{"HOME=" + home})
	want := filepath.Join(home, ".config", "aide", "claude")
	if got["CLAUDE_CONFIG_DIR"] != want {
		t.Errorf("CLAUDE_CONFIG_DIR = %q, want %q", got["CLAUDE_CONFIG_DIR"], want)
	}
}

func TestSandboxAgentEnv_RespectsUserValue(t *testing.T) {
	home := t.TempDir()
	got := SandboxAgentEnv("claude", home, []string{"HOME=" + home, "CLAUDE_CONFIG_DIR=/custom"})
	if _, ok := got["CLAUDE_CONFIG_DIR"]; ok {
		t.Errorf("user-set CLAUDE_CONFIG_DIR should not be overridden, got %v", got)
	}
}

func TestSandboxAgentEnv_NoRedirectAgents(t *testing.T) {
	home := t.TempDir()
	for _, name := range []string{"pi", "opencode", "not-an-agent"} {
		if got := SandboxAgentEnv(name, home, []string{"HOME=" + home}); len(got) != 0 {
			t.Errorf("SandboxAgentEnv(%q) = %v, want empty", name, got)
		}
	}
}
