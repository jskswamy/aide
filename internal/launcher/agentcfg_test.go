package launcher

import (
	"testing"
)

func TestResolveAgentModule_KnownAgents(t *testing.T) {
	for _, name := range []string{"claude", "codex", "aider", "goose", "amp", "gemini"} {
		if mod := ResolveAgentModule(name); mod == nil {
			t.Errorf("ResolveAgentModule(%q) = nil, want non-nil", name)
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
