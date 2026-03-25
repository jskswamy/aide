package sandbox

import (
	"testing"

	"github.com/jskswamy/aide/internal/config"
)

func TestMergeCapNames_ContextOnly(t *testing.T) {
	got := MergeCapNames([]string{"k8s", "docker"}, nil, nil)
	if len(got) != 2 || got[0] != "k8s" || got[1] != "docker" {
		t.Errorf("expected [k8s docker], got %v", got)
	}
}

func TestMergeCapNames_WithFlags(t *testing.T) {
	got := MergeCapNames([]string{"k8s"}, []string{"docker", "ssh"}, nil)
	if len(got) != 3 {
		t.Errorf("expected 3 caps, got %v", got)
	}
}

func TestMergeCapNames_WithoutFlags(t *testing.T) {
	got := MergeCapNames([]string{"k8s", "docker", "ssh"}, nil, []string{"docker"})
	if len(got) != 2 || got[0] != "k8s" || got[1] != "ssh" {
		t.Errorf("expected [k8s ssh], got %v", got)
	}
}

func TestMergeCapNames_WithAndWithout(t *testing.T) {
	got := MergeCapNames([]string{"k8s"}, []string{"docker", "ssh"}, []string{"ssh"})
	if len(got) != 2 || got[0] != "k8s" || got[1] != "docker" {
		t.Errorf("expected [k8s docker], got %v", got)
	}
}

func TestMergeCapNames_Empty(t *testing.T) {
	got := MergeCapNames(nil, nil, nil)
	if len(got) != 0 {
		t.Errorf("expected empty, got %v", got)
	}
}

func TestApplyOverrides_NilConfig(t *testing.T) {
	var cfg *config.SandboxPolicy
	overrides := config.SandboxOverrides{
		Unguard:       []string{"cloud-azure"},
		ReadableExtra: []string{"~/.azure"},
	}
	ApplyOverrides(&cfg, overrides)

	if cfg == nil {
		t.Fatal("expected non-nil config after ApplyOverrides")
	}
	if len(cfg.Unguard) != 1 || cfg.Unguard[0] != "cloud-azure" {
		t.Errorf("expected Unguard [cloud-azure], got %v", cfg.Unguard)
	}
	if len(cfg.ReadableExtra) != 1 || cfg.ReadableExtra[0] != "~/.azure" {
		t.Errorf("expected ReadableExtra [~/.azure], got %v", cfg.ReadableExtra)
	}
}

func TestApplyOverrides_ExistingConfig(t *testing.T) {
	cfg := &config.SandboxPolicy{
		Unguard:     []string{"ssh-keys"},
		ReadableExtra: []string{"~/.ssh"},
	}
	overrides := config.SandboxOverrides{
		Unguard:       []string{"cloud-azure", "terraform"},
		ReadableExtra: []string{"~/.azure", "~/.terraform.d"},
		WritableExtra: []string{"/tmp/tf"},
	}
	ApplyOverrides(&cfg, overrides)

	if len(cfg.Unguard) != 3 {
		t.Errorf("expected 3 unguards, got %v", cfg.Unguard)
	}
	if len(cfg.ReadableExtra) != 3 {
		t.Errorf("expected 3 readable, got %v", cfg.ReadableExtra)
	}
	if len(cfg.WritableExtra) != 1 || cfg.WritableExtra[0] != "/tmp/tf" {
		t.Errorf("expected WritableExtra [/tmp/tf], got %v", cfg.WritableExtra)
	}
}

func TestResolveCapabilities_Empty(t *testing.T) {
	cfg := &config.Config{}
	capSet, overrides, err := ResolveCapabilities(nil, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capSet != nil {
		t.Error("expected nil capSet for empty names")
	}
	if len(overrides.Unguard) != 0 {
		t.Error("expected empty overrides for empty names")
	}
}

func TestResolveCapabilities_BuiltinCaps(t *testing.T) {
	cfg := &config.Config{}
	capSet, overrides, err := ResolveCapabilities([]string{"azure", "terraform"}, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capSet == nil {
		t.Fatal("expected non-nil capSet")
	}

	// Azure unguards cloud-azure, terraform unguards terraform
	unguardSet := make(map[string]bool)
	for _, u := range overrides.Unguard {
		unguardSet[u] = true
	}
	if !unguardSet["cloud-azure"] {
		t.Error("expected cloud-azure in Unguard")
	}
	if !unguardSet["terraform"] {
		t.Error("expected terraform in Unguard")
	}
}

func TestResolveCapabilities_Unknown(t *testing.T) {
	cfg := &config.Config{}
	_, _, err := ResolveCapabilities([]string{"nonexistent"}, cfg)
	if err == nil {
		t.Error("expected error for unknown capability")
	}
}
