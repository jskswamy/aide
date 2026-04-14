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

func TestEnableGuard_MetaGuardRejected(t *testing.T) {
	cfg := &config.SandboxPolicy{}
	r := EnableGuard(cfg, "all-default")
	if r.OK() {
		t.Error("expected error for meta-guard name")
	}
	if len(r.Errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(r.Errors))
	}
}

func TestEnableGuard_AlreadyActiveDefaultGuard(t *testing.T) {
	// aide-secrets is a default guard, already tested in TestEnableGuard_Basic
	// but let's also test with explicit guards list where it's present
	cfg := &config.SandboxPolicy{
		Guards: []string{"aide-secrets", "project-secrets"},
	}
	r := EnableGuard(cfg, "aide-secrets")
	if len(r.Warnings) == 0 {
		t.Error("expected warning for already active guard")
	}
}

func TestEnableGuard_UnguardCleanupKeepsOtherExpanded(t *testing.T) {
	// When unguard has "all-default" (a meta-guard), enabling one default guard
	// should expand it and keep the other default guards in unguard.
	cfg := &config.SandboxPolicy{
		Unguard: []string{"all-default"},
	}
	r := EnableGuard(cfg, "aide-secrets")
	if !r.OK() {
		t.Fatalf("unexpected error: %v", r.Errors)
	}
	// The other default guards should remain in unguard
	for _, u := range cfg.Unguard {
		if u == "aide-secrets" {
			t.Error("aide-secrets should not remain in unguard")
		}
	}
	if len(cfg.Unguard) == 0 {
		t.Error("expected other default guards to remain in unguard")
	}
}

func TestEnableGuard_UnguardCleanupKeepsNonMatching(t *testing.T) {
	// Unguard entries that don't match the target guard should be kept as-is.
	cfg := &config.SandboxPolicy{
		Unguard: []string{"aide-secrets", "dev-credentials"},
	}
	r := EnableGuard(cfg, "aide-secrets")
	if !r.OK() {
		t.Fatalf("unexpected error: %v", r.Errors)
	}
	if len(cfg.Unguard) != 1 || cfg.Unguard[0] != "dev-credentials" {
		t.Errorf("expected Unguard=[dev-credentials], got %v", cfg.Unguard)
	}
}

func TestEnableGuard_AddsToGuardsExtra(t *testing.T) {
	// When guards list is empty, non-default guard should go to guards_extra
	cfg := &config.SandboxPolicy{}
	r := EnableGuard(cfg, "git-remote")
	if !r.OK() {
		t.Fatalf("unexpected error: %v", r.Errors)
	}
	found := false
	for _, g := range cfg.GuardsExtra {
		if g == "git-remote" {
			found = true
		}
	}
	if !found {
		t.Error("git-remote should be added to GuardsExtra when Guards is empty")
	}
	if len(cfg.Guards) != 0 {
		t.Errorf("Guards should remain empty, got %v", cfg.Guards)
	}
}

func TestEnableGuard_AddsToGuards(t *testing.T) {
	// When guards list is non-empty, guard should go to guards
	cfg := &config.SandboxPolicy{
		Guards: []string{"aide-secrets"},
	}
	r := EnableGuard(cfg, "git-remote")
	if !r.OK() {
		t.Fatalf("unexpected error: %v", r.Errors)
	}
	found := false
	for _, g := range cfg.Guards {
		if g == "git-remote" {
			found = true
		}
	}
	if !found {
		t.Error("git-remote should be added to Guards when Guards list exists")
	}
}

func TestDisableGuard_MetaGuardRejected(t *testing.T) {
	cfg := &config.SandboxPolicy{}
	r := DisableGuard(cfg, "all-default")
	if r.OK() {
		t.Error("expected error for meta-guard name")
	}
	if len(r.Errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(r.Errors))
	}
}

func TestDisableGuard_Unknown(t *testing.T) {
	cfg := &config.SandboxPolicy{}
	r := DisableGuard(cfg, "nonexistent")
	if r.OK() {
		t.Error("expected error for unknown guard")
	}
}

func TestDisableGuard_RemovesFromGuards(t *testing.T) {
	cfg := &config.SandboxPolicy{
		Guards: []string{"aide-secrets", "git-remote"},
	}
	r := DisableGuard(cfg, "git-remote")
	if !r.OK() {
		t.Fatalf("unexpected error: %v", r.Errors)
	}
	for _, g := range cfg.Guards {
		if g == "git-remote" {
			t.Error("git-remote should be removed from Guards")
		}
	}
	if len(cfg.Guards) != 1 || cfg.Guards[0] != "aide-secrets" {
		t.Errorf("expected Guards=[aide-secrets], got %v", cfg.Guards)
	}
	// Should NOT be added to unguard since it was in explicit guards list
	if len(cfg.Unguard) != 0 {
		t.Errorf("expected empty Unguard, got %v", cfg.Unguard)
	}
}

func TestDisableGuard_RemovesFromGuardsExtra(t *testing.T) {
	cfg := &config.SandboxPolicy{
		GuardsExtra: []string{"git-remote"},
	}
	r := DisableGuard(cfg, "git-remote")
	if !r.OK() {
		t.Fatalf("unexpected error: %v", r.Errors)
	}
	if len(cfg.GuardsExtra) != 0 {
		t.Errorf("expected empty GuardsExtra, got %v", cfg.GuardsExtra)
	}
	// Should NOT be added to unguard since it was in guards_extra
	if len(cfg.Unguard) != 0 {
		t.Errorf("expected empty Unguard, got %v", cfg.Unguard)
	}
}

func TestRemoveFromSlice(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		val      string
		expected []string
		found    bool
	}{
		{"remove middle", []string{"a", "b", "c"}, "b", []string{"a", "c"}, true},
		{"remove first", []string{"a", "b", "c"}, "a", []string{"b", "c"}, true},
		{"remove last", []string{"a", "b", "c"}, "c", []string{"a", "b"}, true},
		{"not found", []string{"a", "b"}, "z", []string{"a", "b"}, false},
		{"empty slice", []string{}, "a", []string{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := make([]string, len(tt.input))
			copy(s, tt.input)
			got := removeFromSlice(&s, tt.val)
			if got != tt.found {
				t.Errorf("removeFromSlice returned %v, want %v", got, tt.found)
			}
			if len(s) != len(tt.expected) {
				t.Errorf("slice = %v, want %v", s, tt.expected)
				return
			}
			for i := range s {
				if s[i] != tt.expected[i] {
					t.Errorf("slice = %v, want %v", s, tt.expected)
					break
				}
			}
		})
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
