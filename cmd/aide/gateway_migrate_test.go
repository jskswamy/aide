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
	if !strings.Contains(out.String(), "--write converts them") || !strings.Contains(out.String(), "gateway_choice:") {
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

// Adding a gateway to a not-yet-migrated profile migrates it first, so
// the existing choices survive on the single corp gateway.
func TestGatewayAddMigratesLegacyProfileFirst(t *testing.T) {
	e := newGatewayEnv(t)
	cfgDir := filepath.Join(e.xdg, "aide")
	if err := os.MkdirAll(filepath.Join(cfgDir, "profiles"), 0o700); err != nil {
		t.Fatal(err)
	}
	legacy := "agent: claude\nconfig_dir_name: work\ndefault_credentials: [corp]\ngateways:\n" +
		"  corp: {base_url: http://127.0.0.1:1}\n  zeta: {base_url: http://127.0.0.1:2}\n"
	if err := os.WriteFile(filepath.Join(cfgDir, "profiles", "work.yaml"), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := runGateway(t, gwCmdToken+"\n", "add", "newgw", "--url", e.srv.URL, "--profile", "work", "--token-stdin")
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}
	ctx := mustLoadProfile(t, cfgDir, "work")
	for _, h := range []string{"claude", "pi", "omp", "opencode", "codex"} {
		if ctx.GatewayChoice[h] != "corp" {
			t.Errorf("gateway_choice.%s = %q, want corp", h, ctx.GatewayChoice[h])
		}
	}
	if _, dup := ctx.Gateways["corp-codex"]; dup {
		t.Error("unexpected corp-codex gateway")
	}
}

// A second gateway added with the default harnesses serves Codex on itself.
func TestGatewayAddSecondGatewayServesCodexDirectly(t *testing.T) {
	e := newGatewayEnv(t)
	for _, n := range []string{"g1", "g2"} {
		if out, err := runGateway(t, gwCmdToken+"\n", "add", n, "--url", e.srv.URL, "--profile", "work", "--token-stdin"); err != nil {
			t.Fatalf("add %s: %v\n%s", n, err, out)
		}
	}
	ctx := mustLoadProfile(t, filepath.Join(e.xdg, "aide"), "work")
	if g2, ok := ctx.Gateways["g2"]; !ok || strings.Join(g2.Harnesses, ",") != "claude,pi,omp,opencode,codex" {
		t.Errorf("g2 = %+v, %v", g2, ok)
	}
	if _, ok := ctx.Gateways["g2-codex"]; ok {
		t.Errorf("unexpected g2-codex sibling: %+v", ctx.Gateways["g2-codex"])
	}
}

func mustLoadProfile(t *testing.T, cfgDir, name string) config.Context {
	t.Helper()
	cfg, err := config.Load(cfgDir, "")
	if err != nil {
		t.Fatal(err)
	}
	return cfg.Contexts[name]
}
