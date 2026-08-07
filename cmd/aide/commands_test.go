// cmd/aide/commands_test.go
package main

import (
	"os"
	"path/filepath"
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

	name, caps, err := resolveEffectiveCapabilities(cfg, t.TempDir(), "", t.TempDir())
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

	name, caps, err := resolveEffectiveCapabilities(cfg, t.TempDir(), "", t.TempDir())
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

	name, caps, err := resolveEffectiveCapabilities(cfg, t.TempDir(), "other", t.TempDir())
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

	_, _, err := resolveEffectiveCapabilities(cfg, t.TempDir(), "nonexistent", t.TempDir())
	if err == nil {
		t.Fatal("expected error for unknown context, got nil")
	}
}

// TestResolveEffectiveCapabilities_AutoIncludesCcstatusline proves the fix
// for the gap where `aide cap list`/`aide cap audit` could report a
// different effective capability set than what a real `aide launch`
// grants: when ~/.config/ccstatusline/settings.json exists, it must show
// up here too, not just at actual launch time (internal/launcher.Launch).
func TestResolveEffectiveCapabilities_AutoIncludesCcstatusline(t *testing.T) {
	homeDir := t.TempDir()
	ccDir := filepath.Join(homeDir, ".config", "ccstatusline")
	if err := os.MkdirAll(ccDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ccDir, "settings.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		Contexts: map[string]config.Context{
			"work": {
				Capabilities: []string{"go"},
			},
		},
		DefaultContext: "work",
	}

	name, caps, err := resolveEffectiveCapabilities(cfg, t.TempDir(), "", homeDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "work" {
		t.Errorf("expected context name %q, got %q", "work", name)
	}
	want := map[string]bool{"go": true, "ccstatusline": true}
	if len(caps) != len(want) {
		t.Fatalf("expected [go ccstatusline], got %v", caps)
	}
	for _, c := range caps {
		if !want[c] {
			t.Errorf("unexpected capability %q in %v", c, caps)
		}
	}
}
