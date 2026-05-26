package launcher

import (
	"os"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/sandbox"
)

func TestResolveAgentModule_KnownAgents(t *testing.T) {
	for _, name := range []string{"claude", "codex", "aider", "goose", "amp", "gemini", "cursor-agent"} {
		if mod := ResolveAgentModule(name); mod == nil {
			t.Errorf("ResolveAgentModule(%q) = nil, want non-nil", name)
		}
	}
}

// Cursor's installer also drops a shorter "agent" symlink; auto-recognising it
// would shadow other tools, so the resolver must only match "cursor-agent".
func TestResolveAgentModule_AgentSymlinkIsNotRecognised(t *testing.T) {
	for _, name := range []string{"agent", "/usr/local/bin/agent"} {
		if mod := ResolveAgentModule(name); mod != nil {
			t.Errorf("ResolveAgentModule(%q) = %v, want nil (agent alias must not be auto-recognised)", name, mod)
		}
	}
}

func TestResolveAgentModule_UnknownAgent(t *testing.T) {
	if mod := ResolveAgentModule("vim"); mod != nil {
		t.Errorf("ResolveAgentModule(vim) = %v, want nil", mod)
	}
}

func TestResolveAgentModule_PathBasename(t *testing.T) {
	if mod := ResolveAgentModule("/usr/local/bin/claude"); mod == nil {
		t.Error("ResolveAgentModule(/usr/local/bin/claude) = nil, want non-nil")
	}
}

// TestApplyAgentEnv_InjectedKeysReflectedInPolicyEnv verifies that keys added
// by applyAgentEnv (e.g. CLAUDE_CONFIG_DIR) are present in policy.Env after the
// sync that follows the call in launcher.go. Without that sync the re-exec child
// would read a stale policy.Env and resolve capability-guarded paths against the
// default value instead of the injected one, causing silent EACCES.
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
