package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/config"
)

func TestInitCmd_WritesModularSplitConfig(t *testing.T) {
	dir := isolatedConfigDir(t)
	cfgDir := filepath.Join(dir, "xdg", "aide")

	// Place a fake "claude" binary on PATH so ScanAgents detects it deterministically.
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "claude"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)

	cmd := initCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	// Accept default agent ("") and decline secrets ("n").
	cmd.SetIn(strings.NewReader("\nn\n"))

	if err := cmd.Execute(); err != nil {
		t.Fatalf("initCmd: %v\n%s", err, out.String())
	}

	// Verify split files were created: config.yaml + profiles/default.yaml
	rootBytes, err := os.ReadFile(filepath.Join(cfgDir, "config.yaml"))
	if err != nil {
		t.Fatalf("read config.yaml: %v", err)
	}
	if !strings.Contains(string(rootBytes), "default_profile: default") {
		t.Errorf("config.yaml missing default_profile: default:\n%s", rootBytes)
	}
	if strings.Contains(string(rootBytes), "\nagent:") || strings.HasPrefix(string(rootBytes), "agent:") {
		t.Errorf("config.yaml should not be flat minimal config:\n%s", rootBytes)
	}

	profBytes, err := os.ReadFile(filepath.Join(cfgDir, "profiles", "default.yaml"))
	if err != nil {
		t.Fatalf("read profiles/default.yaml: %v", err)
	}
	if !strings.Contains(string(profBytes), "agent: claude") {
		t.Errorf("profiles/default.yaml missing agent: claude:\n%s", profBytes)
	}

	// Loading the initialized config must not be LegacyMonolithic and must allow immediate WriteConfigTo.
	cfg, err := config.Load(cfgDir, dir)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if cfg.LegacyMonolithic {
		t.Errorf("cfg.LegacyMonolithic = true after aide init, want false")
	}
	if got := cfg.Contexts["default"].Profile; got != "" {
		t.Errorf("default profile config_dir_name = %q, want empty (unisolated)", got)
	}
}
