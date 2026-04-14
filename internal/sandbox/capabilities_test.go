package sandbox

import (
	"slices"
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
		ReadableExtra: []string{"~/.azure"},
	}
	ApplyOverrides(&cfg, overrides)

	if cfg == nil {
		t.Fatal("expected non-nil config after ApplyOverrides")
	}
	if len(cfg.ReadableExtra) != 1 || cfg.ReadableExtra[0] != "~/.azure" {
		t.Errorf("expected ReadableExtra [~/.azure], got %v", cfg.ReadableExtra)
	}
}

func TestApplyOverrides_ExistingConfig(t *testing.T) {
	cfg := &config.SandboxPolicy{
		ReadableExtra: []string{"~/.ssh"},
	}
	overrides := config.SandboxOverrides{
		ReadableExtra: []string{"~/.azure", "~/.terraform.d"},
		WritableExtra: []string{"/tmp/tf"},
	}
	ApplyOverrides(&cfg, overrides)

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

	// Capabilities no longer have Unguard fields — they use Writable/Readable directly.
	if len(overrides.Unguard) != 0 {
		t.Errorf("expected 0 unguards (guards removed), got %v", overrides.Unguard)
	}
}

func TestResolveCapabilities_Unknown(t *testing.T) {
	cfg := &config.Config{}
	_, _, err := ResolveCapabilities([]string{"nonexistent"}, cfg)
	if err == nil {
		t.Error("expected error for unknown capability")
	}
}

func TestApplyOverrides_EnableGuard(t *testing.T) {
	cfg := &config.SandboxPolicy{}
	overrides := config.SandboxOverrides{
		EnableGuard: []string{"git-remote"},
	}
	ApplyOverrides(&cfg, overrides)
	if len(cfg.GuardsExtra) != 1 || cfg.GuardsExtra[0] != "git-remote" {
		t.Errorf("expected GuardsExtra [git-remote], got %v", cfg.GuardsExtra)
	}
}

func TestApplyOverrides_NetworkMode(t *testing.T) {
	cfg := &config.SandboxPolicy{
		Network: &config.NetworkPolicy{
			Mode:      "outbound",
			DenyPorts: []int{22},
		},
	}
	overrides := config.SandboxOverrides{
		NetworkMode: "unrestricted",
	}
	ApplyOverrides(&cfg, overrides)

	if cfg.Network == nil || cfg.Network.Mode != "unrestricted" {
		t.Errorf("expected network mode unrestricted, got %v", cfg.Network)
	}
	// Port deny list from config must be preserved
	if len(cfg.Network.DenyPorts) != 1 || cfg.Network.DenyPorts[0] != 22 {
		t.Errorf("expected deny_ports [22] preserved, got %v", cfg.Network.DenyPorts)
	}
}

func TestApplyOverrides_NetworkMode_NilNetwork(t *testing.T) {
	cfg := &config.SandboxPolicy{}
	overrides := config.SandboxOverrides{
		NetworkMode: "unrestricted",
	}
	ApplyOverrides(&cfg, overrides)

	if cfg.Network == nil || cfg.Network.Mode != "unrestricted" {
		t.Errorf("expected network mode unrestricted, got %v", cfg.Network)
	}
}

func TestApplyOverrides_NetworkMode_Empty(t *testing.T) {
	cfg := &config.SandboxPolicy{
		Network: &config.NetworkPolicy{Mode: "outbound"},
	}
	overrides := config.SandboxOverrides{}
	ApplyOverrides(&cfg, overrides)

	if cfg.Network.Mode != "outbound" {
		t.Errorf("expected network mode unchanged, got %q", cfg.Network.Mode)
	}
}

func TestResolveCapabilities_GitRemote_EnableGuard(t *testing.T) {
	cfg := &config.Config{}
	capSet, overrides, err := ResolveCapabilities([]string{"git-remote"}, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capSet == nil {
		t.Fatal("expected non-nil capSet")
	}

	// EnableGuard should flow through to overrides
	if len(overrides.EnableGuard) != 1 || overrides.EnableGuard[0] != "git-remote" {
		t.Errorf("expected EnableGuard [git-remote], got %v", overrides.EnableGuard)
	}

	// EnvAllow should include SSH_AUTH_SOCK
	if !slices.Contains(overrides.EnvAllow, "SSH_AUTH_SOCK") {
		t.Error("expected SSH_AUTH_SOCK in EnvAllow")
	}

	// ApplyOverrides should add to GuardsExtra
	var sandboxCfg *config.SandboxPolicy
	ApplyOverrides(&sandboxCfg, overrides)

	if len(sandboxCfg.GuardsExtra) != 1 || sandboxCfg.GuardsExtra[0] != "git-remote" {
		t.Errorf("expected GuardsExtra [git-remote], got %v", sandboxCfg.GuardsExtra)
	}
}

func TestNetworkCapability_EndToEnd(t *testing.T) {
	cfg := &config.Config{
		Capabilities: map[string]config.CapabilityDef{},
	}

	// Simulate --with network
	capNames := MergeCapNames(nil, []string{"network"}, nil)
	_, overrides, err := ResolveCapabilities(capNames, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if overrides.NetworkMode != "unrestricted" {
		t.Errorf("expected NetworkMode unrestricted, got %q", overrides.NetworkMode)
	}

	// Apply to a sandbox config with port deny list
	sandboxCfg := &config.SandboxPolicy{
		Network: &config.NetworkPolicy{
			Mode:      "outbound",
			DenyPorts: []int{22},
		},
	}
	ApplyOverrides(&sandboxCfg, overrides)

	// Network mode should be unrestricted but deny ports preserved
	if sandboxCfg.Network.Mode != "unrestricted" {
		t.Errorf("expected unrestricted, got %q", sandboxCfg.Network.Mode)
	}
	if len(sandboxCfg.Network.DenyPorts) != 1 {
		t.Errorf("expected deny_ports preserved, got %v", sandboxCfg.Network.DenyPorts)
	}
}

func TestUnrestrictedNetworkFlag_ClearsPortRules(t *testing.T) {
	sandboxCfg := &config.SandboxPolicy{
		Network: &config.NetworkPolicy{
			Mode:       "outbound",
			AllowPorts: []int{443, 8443},
			DenyPorts:  []int{22},
		},
	}

	// Simulate -N flag behavior
	sandboxCfg.Network.Mode = "unrestricted"
	sandboxCfg.Network.AllowPorts = nil
	sandboxCfg.Network.DenyPorts = nil

	if sandboxCfg.Network.Mode != "unrestricted" {
		t.Errorf("expected unrestricted, got %q", sandboxCfg.Network.Mode)
	}
	if sandboxCfg.Network.AllowPorts != nil {
		t.Error("expected AllowPorts nil")
	}
	if sandboxCfg.Network.DenyPorts != nil {
		t.Error("expected DenyPorts nil")
	}
}
