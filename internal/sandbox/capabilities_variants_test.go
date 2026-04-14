package sandbox

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/jskswamy/aide/internal/capability"
	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/consent"
)

func TestResolveCapabilitiesWithVariants_AppliesCLIOverrides(t *testing.T) {
	cfg := &config.Config{}
	cstore := consent.NewStore(t.TempDir())
	opts := VariantSelectionOptions{
		ProjectRoot:  t.TempDir(),
		CLIOverrides: map[string][]string{"python": {"uv"}},
		Consent:      cstore,
	}
	_, overrides, err := ResolveCapabilitiesWithVariants([]string{"python"}, cfg, opts)
	if err != nil {
		t.Fatal(err)
	}
	gotWritable := false
	for _, w := range overrides.WritableExtra {
		if w == "~/.local/share/uv" {
			gotWritable = true
		}
	}
	if !gotWritable {
		t.Errorf("uv variant's writable not applied; got %v", overrides.WritableExtra)
	}
}

func TestResolveCapabilitiesWithVariants_NoDetection_UsesDefault(t *testing.T) {
	cfg := &config.Config{}
	opts := VariantSelectionOptions{
		ProjectRoot: t.TempDir(), // no markers
		Consent:     consent.NewStore(t.TempDir()),
	}
	_, overrides, err := ResolveCapabilitiesWithVariants([]string{"python"}, cfg, opts)
	if err != nil {
		t.Fatal(err)
	}
	// venv is the default and contributes VIRTUAL_ENV env.
	gotEnv := false
	for _, e := range overrides.EnvAllow {
		if e == "VIRTUAL_ENV" {
			gotEnv = true
		}
	}
	if !gotEnv {
		t.Errorf("venv default env not applied; got %v", overrides.EnvAllow)
	}
}

func TestResolveCapabilitiesWithVariants_AutoDetect_UVMarker(t *testing.T) {
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "uv.lock"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	opts := VariantSelectionOptions{
		ProjectRoot: project,
		Consent:     consent.NewStore(t.TempDir()),
		AutoYes:     true, // auto-approve in test
		Interactive: true,
	}
	_, overrides, err := ResolveCapabilitiesWithVariants([]string{"python"}, cfg, opts)
	if err != nil {
		t.Fatal(err)
	}
	got := false
	for _, w := range overrides.WritableExtra {
		if w == "~/.local/share/uv" {
			got = true
		}
	}
	if !got {
		t.Errorf("uv detected path not merged; got %v", overrides.WritableExtra)
	}
}

func TestResolveCapabilitiesWithVariants_UnknownVariant_ReturnsError(t *testing.T) {
	cfg := &config.Config{}
	opts := VariantSelectionOptions{
		ProjectRoot:  t.TempDir(),
		CLIOverrides: map[string][]string{"python": {"nosuch"}},
		Consent:      consent.NewStore(t.TempDir()),
	}
	_, _, err := ResolveCapabilitiesWithVariants([]string{"python"}, cfg, opts)
	if err == nil {
		t.Fatal("want error, got nil")
	}
	var uve *capability.UnknownVariantError
	if !errors.As(err, &uve) {
		t.Errorf("err = %v, want *capability.UnknownVariantError", err)
	}
}
