package launcher

import (
	"testing"

	"github.com/jskswamy/aide/internal/capability"
	"github.com/jskswamy/aide/internal/config"
)

func TestBuildEnvItems_ContextOnly(t *testing.T) {
	items := BuildEnvItems(
		map[string]string{
			"ANTHROPIC_MODEL":   "claude-sonnet-4-6",
			"ANTHROPIC_API_KEY": "{{ .secrets.api_key }}",
		},
		nil, nil, nil,
	)
	if len(items) != 2 {
		t.Fatalf("len = %d, want 2", len(items))
	}
	// sorted by key: ANTHROPIC_API_KEY first
	if items[0].Key != "ANTHROPIC_API_KEY" {
		t.Errorf("items[0].Key = %q, want ANTHROPIC_API_KEY", items[0].Key)
	}
	if items[0].Badge != "🔐" {
		t.Errorf("items[0].Badge = %q, want 🔐", items[0].Badge)
	}
	if items[1].Badge != "📌" {
		t.Errorf("items[1].Badge = %q, want 📌", items[1].Badge)
	}
	if items[1].Annotation != "= claude-sonnet-4-6" {
		t.Errorf("items[1].Annotation = %q, want = claude-sonnet-4-6", items[1].Annotation)
	}
}

func TestBuildEnvItems_CapabilityEnv(t *testing.T) {
	caps := []capability.ResolvedCapability{
		{Name: "aws", EnvAllow: []string{"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY"}},
	}
	items := BuildEnvItems(nil, caps, nil, nil)
	if len(items) != 2 {
		t.Fatalf("len = %d, want 2", len(items))
	}
	for _, item := range items {
		if item.Badge != "🔧" {
			t.Errorf("%s badge = %q, want 🔧", item.Key, item.Badge)
		}
		if item.Annotation != "← aws" {
			t.Errorf("%s annotation = %q, want ← aws", item.Key, item.Annotation)
		}
	}
	var found bool
	for _, item := range items {
		if item.Key == "AWS_SECRET_ACCESS_KEY" && item.CredWarning {
			found = true
		}
	}
	if !found {
		t.Error("AWS_SECRET_ACCESS_KEY should have CredWarning=true")
	}
}

func TestBuildEnvItems_NeverAllow(t *testing.T) {
	items := BuildEnvItems(
		nil,
		[]capability.ResolvedCapability{{Name: "aws", EnvAllow: []string{"AWS_SECRET_ACCESS_KEY"}}},
		[]string{"AWS_SECRET_ACCESS_KEY"},
		nil,
	)
	if len(items) != 1 {
		t.Fatalf("len = %d, want 1", len(items))
	}
	item := items[0]
	if !item.Blocked {
		t.Error("item should be Blocked")
	}
	if item.Badge != "⊘" {
		t.Errorf("badge = %q, want ⊘", item.Badge)
	}
	if item.Annotation != "never-allow" {
		t.Errorf("annotation = %q, want never-allow", item.Annotation)
	}
}

func TestBuildEnvItems_DetailedMode(t *testing.T) {
	items := BuildEnvItems(
		map[string]string{"API_KEY": "{{ .secrets.api_key }}"},
		nil, nil,
		map[string]string{"API_KEY": "sk-ant-abc123xyz"},
	)
	if len(items) != 1 {
		t.Fatalf("len = %d, want 1", len(items))
	}
	// RedactValue shows first 8 chars + ***
	if items[0].ResolvedValue != "sk-ant-a***" {
		t.Errorf("ResolvedValue = %q, want sk-ant-a***", items[0].ResolvedValue)
	}
}

func TestBuildEnvItems_ContextTakesPrecedenceOverCap(t *testing.T) {
	items := BuildEnvItems(
		map[string]string{"MY_VAR": "explicit-value"},
		[]capability.ResolvedCapability{{Name: "mycap", EnvAllow: []string{"MY_VAR"}}},
		nil, nil,
	)
	count := 0
	for _, item := range items {
		if item.Key == "MY_VAR" {
			count++
			if item.Badge != "📌" {
				t.Errorf("MY_VAR badge = %q, want 📌 (context wins)", item.Badge)
			}
		}
	}
	if count != 1 {
		t.Errorf("MY_VAR appears %d times, want 1", count)
	}
}

func TestResolveAgentIcon_ConfigOverride(t *testing.T) {
	def := &config.AgentDef{Binary: "claude", Icon: "🚀"}
	got := ResolveAgentIcon("claude", def)
	if got != "🚀" {
		t.Errorf("got %q, want 🚀", got)
	}
}

func TestResolveAgentIcon_DefaultFallback(t *testing.T) {
	got := ResolveAgentIcon("claude", nil)
	if got != "🤖" {
		t.Errorf("got %q, want 🤖", got)
	}
}

func TestResolveAgentIcon_UnknownAgent(t *testing.T) {
	got := ResolveAgentIcon("unknown-tool", nil)
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}
