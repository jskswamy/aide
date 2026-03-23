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
	r := EnableGuard(cfg, "docker")
	if !r.OK() {
		t.Errorf("unexpected error: %v", r.Errors)
	}
	found := false
	for _, g := range cfg.GuardsExtra {
		if g == "docker" {
			found = true
		}
	}
	if !found {
		t.Error("docker should be in guards_extra")
	}
}

func TestEnableGuard_WithGuardsSet(t *testing.T) {
	cfg := &config.SandboxPolicy{
		Guards: []string{"ssh-keys"},
	}
	r := EnableGuard(cfg, "docker")
	if !r.OK() {
		t.Errorf("unexpected error: %v", r.Errors)
	}
	found := false
	for _, g := range cfg.Guards {
		if g == "docker" {
			found = true
		}
	}
	if !found {
		t.Error("docker should be appended to guards (not guards_extra) when guards is set")
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
	r := DisableGuard(cfg, "docker") // opt-in, already inactive
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
