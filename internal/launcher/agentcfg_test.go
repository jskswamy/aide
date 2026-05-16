package launcher

import (
	"testing"
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
