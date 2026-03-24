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
		Unguard: []string{"browsers"},
	}
	result := EffectiveGuards(cfg)
	for _, g := range result {
		if g == "browsers" {
			t.Error("browsers should not be in effective guards after unguard")
		}
	}
}

func TestEnableGuard_Basic(t *testing.T) {
	cfg := &config.SandboxPolicy{}
	r := EnableGuard(cfg, "vercel")
	if !r.OK() {
		t.Errorf("unexpected error: %v", r.Errors)
	}
	found := false
	for _, g := range cfg.GuardsExtra {
		if g == "vercel" {
			found = true
		}
	}
	if !found {
		t.Error("vercel should be in guards_extra")
	}
}

func TestEnableGuard_WithGuardsSet(t *testing.T) {
	cfg := &config.SandboxPolicy{
		Guards: []string{"ssh-keys"},
	}
	r := EnableGuard(cfg, "vercel")
	if !r.OK() {
		t.Errorf("unexpected error: %v", r.Errors)
	}
	found := false
	for _, g := range cfg.Guards {
		if g == "vercel" {
			found = true
		}
	}
	if !found {
		t.Error("vercel should be appended to guards (not guards_extra) when guards is set")
	}
}

func TestEnableGuard_AlreadyActive(t *testing.T) {
	cfg := &config.SandboxPolicy{}
	r := EnableGuard(cfg, "ssh-keys") // ssh-keys is default-active
	if len(r.Warnings) == 0 {
		t.Error("expected warning for already active guard")
	}
}

func TestEnableGuard_MetaGuard(t *testing.T) {
	cfg := &config.SandboxPolicy{}
	r := EnableGuard(cfg, "cloud")
	if r.OK() {
		t.Error("expected error for meta-guard name")
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
	r := DisableGuard(cfg, "browsers")
	if !r.OK() {
		t.Errorf("unexpected error: %v", r.Errors)
	}
	found := false
	for _, g := range cfg.Unguard {
		if g == "browsers" {
			found = true
		}
	}
	if !found {
		t.Error("browsers should be in unguard")
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
	cfg := &config.SandboxPolicy{}
	r := DisableGuard(cfg, "vercel") // opt-in, already inactive
	if len(r.Warnings) == 0 {
		t.Error("expected warning for already inactive guard")
	}
}

func TestDisableGuard_MetaGuard(t *testing.T) {
	cfg := &config.SandboxPolicy{}
	r := DisableGuard(cfg, "cloud")
	if r.OK() {
		t.Error("expected error for meta-guard name")
	}
}

func TestEnableGuard_RemovesFromUnguard(t *testing.T) {
	cfg := &config.SandboxPolicy{
		Unguard: []string{"browsers"},
	}
	r := EnableGuard(cfg, "browsers")
	if !r.OK() {
		t.Errorf("unexpected error: %v", r.Errors)
	}
	for _, u := range cfg.Unguard {
		if u == "browsers" {
			t.Error("browsers should be removed from Unguard")
		}
	}
	// Default guard: removing from unguard is sufficient, should NOT be in GuardsExtra
	for _, g := range cfg.GuardsExtra {
		if g == "browsers" {
			t.Error("default guard should not be added to GuardsExtra when removing from Unguard suffices")
		}
	}
}

func TestEnableGuard_ExpandsMetaGuardInUnguard(t *testing.T) {
	cfg := &config.SandboxPolicy{
		Unguard: []string{"cloud"},
	}
	r := EnableGuard(cfg, "cloud-aws")
	if !r.OK() {
		t.Errorf("unexpected error: %v", r.Errors)
	}
	// "cloud" should be expanded, cloud-aws removed, 4 others kept
	for _, u := range cfg.Unguard {
		if u == "cloud" {
			t.Error("cloud meta-guard should be expanded into individual guards")
		}
		if u == "cloud-aws" {
			t.Error("cloud-aws should not be in Unguard")
		}
	}
	if len(cfg.Unguard) != 4 {
		t.Errorf("expected 4 remaining cloud guards in Unguard, got %d: %v", len(cfg.Unguard), cfg.Unguard)
	}
}

func TestDefaultGuard_DisableEnableRoundTrip(t *testing.T) {
	cfg := &config.SandboxPolicy{}

	// Disable default guard
	r := DisableGuard(cfg, "browsers")
	if !r.OK() {
		t.Fatalf("disable: %v", r.Errors)
	}
	if len(cfg.Unguard) != 1 || cfg.Unguard[0] != "browsers" {
		t.Fatalf("expected Unguard=[browsers], got %v", cfg.Unguard)
	}

	// Re-enable — should just remove from Unguard, not add to GuardsExtra
	r = EnableGuard(cfg, "browsers")
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
	r = DisableGuard(cfg, "browsers")
	if !r.OK() {
		t.Fatalf("disable again: %v", r.Errors)
	}
	if len(cfg.Unguard) != 1 {
		t.Errorf("expected Unguard=[browsers], got %v", cfg.Unguard)
	}
}

func TestOptInGuard_EnableDisableRoundTrip(t *testing.T) {
	cfg := &config.SandboxPolicy{}

	// Enable opt-in
	r := EnableGuard(cfg, "vercel")
	if !r.OK() {
		t.Fatalf("enable: %v", r.Errors)
	}
	if len(cfg.GuardsExtra) != 1 || cfg.GuardsExtra[0] != "vercel" {
		t.Fatalf("expected GuardsExtra=[vercel], got %v", cfg.GuardsExtra)
	}

	// Disable — removes from GuardsExtra
	r = DisableGuard(cfg, "vercel")
	if !r.OK() {
		t.Fatalf("disable: %v", r.Errors)
	}
	if len(cfg.GuardsExtra) != 0 {
		t.Errorf("expected empty GuardsExtra, got %v", cfg.GuardsExtra)
	}
	if len(cfg.Unguard) != 0 {
		t.Errorf("expected empty Unguard (opt-in returns to default inactive), got %v", cfg.Unguard)
	}

	// Re-enable — should work cleanly
	r = EnableGuard(cfg, "vercel")
	if !r.OK() {
		t.Fatalf("re-enable: %v", r.Errors)
	}
	if len(cfg.GuardsExtra) != 1 {
		t.Errorf("expected GuardsExtra=[vercel], got %v", cfg.GuardsExtra)
	}
}
