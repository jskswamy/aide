package main

import (
	"testing"

	"github.com/jskswamy/aide/internal/config"
)

func boolPtrST(b bool) *bool { return &b }

func TestRenderStatusline_AllModulesDefault(t *testing.T) {
	cfg := config.ResolveStatusline(nil, nil)
	env := map[string]string{
		"AIDE_SANDBOX":      "on",
		"AIDE_NETWORK_MODE": "outbound",
		"AIDE_CAPS":         "k8s,docker",
		"AIDE_TRUST":        "trusted",
		"AIDE_CONTEXT":      "my-project",
	}
	got := renderStatusline(cfg, env)
	want := "🔒 | 🌐 | ⚡ k8s,docker | 📁 my-project"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestRenderStatusline_UntrustedShowsTrust(t *testing.T) {
	cfg := config.ResolveStatusline(nil, nil)
	env := map[string]string{
		"AIDE_SANDBOX":      "on",
		"AIDE_NETWORK_MODE": "outbound",
		"AIDE_CAPS":         "",
		"AIDE_TRUST":        "untrusted",
	}
	got := renderStatusline(cfg, env)
	want := "🔒 | 🌐 | ⚠️"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestRenderStatusline_AutoApprovePrepended(t *testing.T) {
	cfg := config.ResolveStatusline(nil, nil)
	env := map[string]string{
		"AIDE_SANDBOX":      "on",
		"AIDE_NETWORK_MODE": "outbound",
		"AIDE_CAPS":         "",
		"AIDE_TRUST":        "trusted",
		"AIDE_AUTO_APPROVE": "1",
	}
	got := renderStatusline(cfg, env)
	want := "🚨 | 🔒 | 🌐"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestRenderStatusline_DisabledModuleSkipped(t *testing.T) {
	project := &config.StatuslineConfig{
		Network: &config.ModuleConfig{Disabled: boolPtrST(true)},
	}
	cfg := config.ResolveStatusline(nil, project)
	env := map[string]string{
		"AIDE_SANDBOX":      "on",
		"AIDE_NETWORK_MODE": "outbound",
		"AIDE_CAPS":         "",
		"AIDE_TRUST":        "trusted",
	}
	got := renderStatusline(cfg, env)
	want := "🔒"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestRenderStatusline_SandboxOff(t *testing.T) {
	cfg := config.ResolveStatusline(nil, nil)
	env := map[string]string{
		"AIDE_SANDBOX":      "off",
		"AIDE_NETWORK_MODE": "unrestricted",
		"AIDE_CAPS":         "",
		"AIDE_TRUST":        "trusted",
	}
	got := renderStatusline(cfg, env)
	want := "🔓 | 🌍"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestRenderStatusline_EmptyCapHidesModule(t *testing.T) {
	cfg := config.ResolveStatusline(nil, nil)
	env := map[string]string{
		"AIDE_SANDBOX":      "on",
		"AIDE_NETWORK_MODE": "outbound",
		"AIDE_CAPS":         "",
		"AIDE_TRUST":        "trusted",
	}
	got := renderStatusline(cfg, env)
	want := "🔒 | 🌐"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestRenderStatusline_CustomOrderRespected(t *testing.T) {
	project := &config.StatuslineConfig{
		Order: []string{"network", "sandbox"},
	}
	cfg := config.ResolveStatusline(nil, project)
	env := map[string]string{
		"AIDE_SANDBOX":      "on",
		"AIDE_NETWORK_MODE": "outbound",
		"AIDE_CAPS":         "",
		"AIDE_TRUST":        "trusted",
	}
	got := renderStatusline(cfg, env)
	want := "🌐 | 🔒"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestRenderStatusline_EmptyResultWhenAllHidden(t *testing.T) {
	project := &config.StatuslineConfig{
		Order: []string{},
	}
	cfg := config.ResolveStatusline(nil, project)
	got := renderStatusline(cfg, map[string]string{})
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}
