// cmd/aide/commands_test.go
package main

import (
	"testing"

	"github.com/jskswamy/aide/internal/config"
)

func TestResolveEffectiveCapabilities_DefaultContextNoOverride(t *testing.T) {
	cfg := &config.Config{
		Contexts: map[string]config.Context{
			"work": {
				Capabilities: []string{"go", "docker"},
			},
		},
		DefaultContext: "work",
	}

	name, caps, err := resolveEffectiveCapabilities(cfg, t.TempDir(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "work" {
		t.Errorf("expected context name %q, got %q", "work", name)
	}
	if len(caps) != 2 || caps[0] != "go" || caps[1] != "docker" {
		t.Errorf("expected [go docker], got %v", caps)
	}
}

func TestResolveEffectiveCapabilities_MergesProjectOverride(t *testing.T) {
	cfg := &config.Config{
		Contexts: map[string]config.Context{
			"work": {
				Capabilities: []string{"go", "docker"},
			},
		},
		DefaultContext: "work",
		ProjectOverride: &config.ProjectOverride{
			Capabilities:         []string{"clipboard"},
			DisabledCapabilities: []string{"docker"},
		},
	}

	name, caps, err := resolveEffectiveCapabilities(cfg, t.TempDir(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "work" {
		t.Errorf("expected context name %q, got %q", "work", name)
	}
	want := map[string]bool{"go": true, "clipboard": true}
	if len(caps) != len(want) {
		t.Fatalf("expected 2 capabilities, got %v", caps)
	}
	for _, c := range caps {
		if !want[c] {
			t.Errorf("unexpected capability %q in %v", c, caps)
		}
	}
}

func TestResolveEffectiveCapabilities_ExplicitContextSkipsOverride(t *testing.T) {
	cfg := &config.Config{
		Contexts: map[string]config.Context{
			"other": {
				Capabilities: []string{"go"},
			},
		},
		DefaultContext: "other",
		ProjectOverride: &config.ProjectOverride{
			Capabilities: []string{"clipboard"},
		},
	}

	name, caps, err := resolveEffectiveCapabilities(cfg, t.TempDir(), "other")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "other" {
		t.Errorf("expected context name %q, got %q", "other", name)
	}
	if len(caps) != 1 || caps[0] != "go" {
		t.Errorf("expected [go] (no project override merge for explicit --context), got %v", caps)
	}
}

func TestResolveEffectiveCapabilities_UnknownContextErrors(t *testing.T) {
	cfg := &config.Config{
		Contexts: map[string]config.Context{
			"work": {},
		},
	}

	_, _, err := resolveEffectiveCapabilities(cfg, t.TempDir(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown context, got nil")
	}
}
