package launcher

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jskswamy/aide/internal/config"
)

func TestPreviewSessionEnv_MinimalConfig_ResolvesSyntheticDefaultContext(t *testing.T) {
	configDir := t.TempDir()
	cwd := t.TempDir()
	homeDir := t.TempDir()

	writeMinimalConfig(t, configDir, `
agent: /usr/local/bin/my-agent
`)
	cfg, err := config.Load(configDir, cwd)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}

	env, err := PreviewSessionEnv(cfg, cwd, "", homeDir)
	if err != nil {
		t.Fatalf("PreviewSessionEnv: %v", err)
	}
	if env["AIDE_CONTEXT"] != "default" {
		t.Errorf("AIDE_CONTEXT = %q, want default", env["AIDE_CONTEXT"])
	}
	if env["AIDE_SANDBOX"] != "on" {
		t.Errorf("AIDE_SANDBOX = %q, want on (default policy)", env["AIDE_SANDBOX"])
	}
	if env["AIDE_NETWORK_MODE"] != "outbound" {
		t.Errorf("AIDE_NETWORK_MODE = %q, want outbound (default policy)", env["AIDE_NETWORK_MODE"])
	}
}

func TestPreviewSessionEnv_NamedContext_NoProjectOverrideMerge(t *testing.T) {
	configDir := t.TempDir()
	cwd := t.TempDir()
	homeDir := t.TempDir()

	writeMinimalConfig(t, configDir, `
contexts:
  work:
    agent: gemini
    match:
      - path: /never/matches
  home:
    agent: claude
    match:
      - path: /never/matches
`)
	cfg, err := config.Load(configDir, cwd)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}

	env, err := PreviewSessionEnv(cfg, cwd, "work", homeDir)
	if err != nil {
		t.Fatalf("PreviewSessionEnv: %v", err)
	}
	if env["AIDE_AGENT"] != "gemini" {
		t.Errorf("AIDE_AGENT = %q, want gemini", env["AIDE_AGENT"])
	}
	if env["AIDE_CONTEXT"] != "work" {
		t.Errorf("AIDE_CONTEXT = %q, want work", env["AIDE_CONTEXT"])
	}
}

func TestPreviewSessionEnv_UnknownContextName_ReturnsError(t *testing.T) {
	configDir := t.TempDir()
	cwd := t.TempDir()
	homeDir := t.TempDir()

	writeMinimalConfig(t, configDir, `
contexts:
  work:
    agent: claude
    match:
      - path: /never/matches
`)
	cfg, err := config.Load(configDir, cwd)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}

	_, err = PreviewSessionEnv(cfg, cwd, "nonexistent", homeDir)
	if err == nil {
		t.Fatal("expected error for unknown context name, got nil")
	}
}

func TestPreviewSessionEnv_SandboxDisabled(t *testing.T) {
	configDir := t.TempDir()
	cwd := t.TempDir()
	homeDir := t.TempDir()

	writeMinimalConfig(t, configDir, `
agent: /usr/local/bin/my-agent
sandbox: false
`)
	cfg, err := config.Load(configDir, cwd)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}

	env, err := PreviewSessionEnv(cfg, cwd, "", homeDir)
	if err != nil {
		t.Fatalf("PreviewSessionEnv: %v", err)
	}
	if env["AIDE_SANDBOX"] != "off" {
		t.Errorf("AIDE_SANDBOX = %q, want off", env["AIDE_SANDBOX"])
	}
}

func TestPreviewSessionEnv_CapsIncludeAutoIncludedCcstatusline(t *testing.T) {
	configDir := t.TempDir()
	cwd := t.TempDir()
	homeDir := t.TempDir()

	ccDir := filepath.Join(homeDir, ".config", "ccstatusline")
	if err := os.MkdirAll(ccDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ccDir, "settings.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	writeMinimalConfig(t, configDir, `
contexts:
  work:
    agent: claude
    capabilities:
      - k8s
    match:
      - path: /never/matches
`)
	cfg, err := config.Load(configDir, cwd)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}

	env, err := PreviewSessionEnv(cfg, cwd, "work", homeDir)
	if err != nil {
		t.Fatalf("PreviewSessionEnv: %v", err)
	}
	want := "k8s,ccstatusline"
	if env["AIDE_CAPS"] != want {
		t.Errorf("AIDE_CAPS = %q, want %q", env["AIDE_CAPS"], want)
	}
}
