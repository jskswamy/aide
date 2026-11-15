package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/config"
)

// legacyGatewayDir writes a split-layout config whose profile still has
// a legacy gateway and returns its directory and home.
func legacyGatewayDir(t *testing.T) (cfgDir, home string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", "")
	cfgDir = filepath.Join(home, "aide")
	if err := os.MkdirAll(filepath.Join(cfgDir, "profiles"), 0o700); err != nil {
		t.Fatal(err)
	}
	for rel, body := range map[string]string{
		"config.yaml":        "agents:\n  claude: {binary: claude}\ndefault_profile: work\n",
		"profiles/work.yaml": "agent: claude\nconfig_dir_name: work\ngateways:\n  corp:\n    base_url: http://127.0.0.1:1\n    harnesses: [claude]\n",
	} {
		if err := os.WriteFile(filepath.Join(cfgDir, rel), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return cfgDir, home
}

// A save of a config with legacy gateways upgrades them first, with
// backups, instead of failing or writing the old shape.
func TestSaveConfigMigratesLegacyGateways(t *testing.T) {
	cfgDir, home := legacyGatewayDir(t)
	cfg, err := config.Load(cfgDir, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := config.WriteConfigTo(cfg, filepath.Join(cfgDir, "config.yaml")); !errors.Is(err, config.ErrNeedsGatewayMigration) {
		t.Fatalf("WriteConfigTo() = %v, want ErrNeedsGatewayMigration", err)
	}
	var errOut bytes.Buffer
	if err := saveConfigWithMigrationPromptIO(cfg, filepath.Join(cfgDir, "config.yaml"), home, strings.NewReader(""), &errOut, false); err != nil {
		t.Fatalf("save = %v", err)
	}
	if !strings.Contains(errOut.String(), "migrated gateway config") {
		t.Errorf("errOut = %q", errOut.String())
	}
	body, _ := os.ReadFile(filepath.Join(cfgDir, "profiles", "work.yaml"))
	if strings.Contains(string(body), "base_url") || !strings.Contains(string(body), "gateway_choice:") {
		t.Errorf("work.yaml after save:\n%s", body)
	}
}

func TestConfigMigratePreviewsThenWritesGateways(t *testing.T) {
	cfgDir, home := legacyGatewayDir(t)
	cfgPath := filepath.Join(cfgDir, "config.yaml")
	statePath := filepath.Join(home, "state.json")
	var out bytes.Buffer
	if err := runConfigMigrate(&out, cfgPath, statePath, home, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "--write converts them") {
		t.Errorf("preview = %q", out.String())
	}
	if b, _ := os.ReadFile(filepath.Join(cfgDir, "profiles", "work.yaml")); !strings.Contains(string(b), "base_url") {
		t.Fatal("the preview wrote")
	}
	out.Reset()
	if err := runConfigMigrate(&out, cfgPath, statePath, home, true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "backups in") {
		t.Errorf("write output = %q", out.String())
	}
	if b, _ := os.ReadFile(filepath.Join(cfgDir, "profiles", "work.yaml")); strings.Contains(string(b), "base_url") {
		t.Errorf("work.yaml still legacy:\n%s", b)
	}
}
