package sandbox

import (
	"testing"

	"github.com/jskswamy/aide/internal/config"
)

func TestEffectiveGuards_Nil(t *testing.T) {
	result := EffectiveGuards(nil)
	if len(result) == 0 {
		t.Fatal("expected default guards for nil config")
	}
}

func TestEffectiveGuards_WithUnguard(t *testing.T) {
	cfg := &config.SandboxPolicy{
		Unguard: []string{"aide-secrets"},
	}
	result := EffectiveGuards(cfg)
	for _, g := range result {
		if g == "aide-secrets" {
			t.Error("aide-secrets should not be in effective guards after unguard")
		}
	}
}

func TestEnableGuard_Basic(t *testing.T) {
	cfg := &config.SandboxPolicy{}
	// aide-secrets is a default guard, so enabling it when already active gives a warning
	r := EnableGuard(cfg, "aide-secrets")
	if len(r.Warnings) == 0 {
		t.Error("expected warning for already active default guard")
	}
}

func TestEnableGuard_Unknown(t *testing.T) {
	cfg := &config.SandboxPolicy{}
	r := EnableGuard(cfg, "nonexistent")
	if r.OK() {
		t.Error("expected error for unknown guard")
	}
}

func TestDisableGuard_Basic(t *testing.T) {
	cfg := &config.SandboxPolicy{}
	r := DisableGuard(cfg, "aide-secrets")
	if !r.OK() {
		t.Errorf("unexpected error: %v", r.Errors)
	}
	found := false
	for _, g := range cfg.Unguard {
		if g == "aide-secrets" {
			found = true
		}
	}
	if !found {
		t.Error("aide-secrets should be in unguard")
	}
}

func TestDisableGuard_Always(t *testing.T) {
	cfg := &config.SandboxPolicy{}
	r := DisableGuard(cfg, "base")
	if r.OK() {
		t.Error("expected error for always guard")
	}
}

func TestDisableGuard_AlreadyInactive(t *testing.T) {
	cfg := &config.SandboxPolicy{
		Unguard: []string{"aide-secrets"},
	}
	r := DisableGuard(cfg, "aide-secrets")
	if len(r.Warnings) == 0 {
		t.Error("expected warning for already inactive guard")
	}
}

func TestEnableGuard_RemovesFromUnguard(t *testing.T) {
	cfg := &config.SandboxPolicy{
		Unguard: []string{"aide-secrets"},
	}
	r := EnableGuard(cfg, "aide-secrets")
	if !r.OK() {
		t.Errorf("unexpected error: %v", r.Errors)
	}
	for _, u := range cfg.Unguard {
		if u == "aide-secrets" {
			t.Error("aide-secrets should be removed from Unguard")
		}
	}
	// Default guard: removing from unguard is sufficient, should NOT be in GuardsExtra
	for _, g := range cfg.GuardsExtra {
		if g == "aide-secrets" {
			t.Error("default guard should not be added to GuardsExtra when removing from Unguard suffices")
		}
	}
}

func TestDefaultGuard_DisableEnableRoundTrip(t *testing.T) {
	cfg := &config.SandboxPolicy{}

	// Disable default guard
	r := DisableGuard(cfg, "project-secrets")
	if !r.OK() {
		t.Fatalf("disable: %v", r.Errors)
	}
	if len(cfg.Unguard) != 1 || cfg.Unguard[0] != "project-secrets" {
		t.Fatalf("expected Unguard=[project-secrets], got %v", cfg.Unguard)
	}

	// Re-enable — should just remove from Unguard, not add to GuardsExtra
	r = EnableGuard(cfg, "project-secrets")
	if !r.OK() {
		t.Fatalf("re-enable: %v", r.Errors)
	}
	if len(cfg.Unguard) != 0 {
		t.Errorf("expected empty Unguard after re-enable, got %v", cfg.Unguard)
	}
	if len(cfg.GuardsExtra) != 0 {
		t.Errorf("expected empty GuardsExtra (default guard), got %v", cfg.GuardsExtra)
	}

	// Disable again — should work cleanly
	r = DisableGuard(cfg, "project-secrets")
	if !r.OK() {
		t.Fatalf("disable again: %v", r.Errors)
	}
	if len(cfg.Unguard) != 1 {
		t.Errorf("expected Unguard=[project-secrets], got %v", cfg.Unguard)
	}
}
