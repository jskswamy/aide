package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jskswamy/aide/internal/config"
)

func TestMaybeNudgeConfigMigration_RespectsSevenDayCooldown(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))

	cfg := &config.Config{LegacyMonolithic: true}
	t0 := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

	var errOut bytes.Buffer
	maybeNudgeConfigMigration(cfg, &errOut, true, home, t0)
	if !strings.Contains(errOut.String(), "aide config migrate --write") {
		t.Fatalf("expected first nudge on TTY, got %q", errOut.String())
	}

	// 2 days later -> silent
	errOut.Reset()
	maybeNudgeConfigMigration(cfg, &errOut, true, home, t0.Add(48*time.Hour))
	if errOut.Len() != 0 {
		t.Fatalf("expected cooldown to suppress nudge, got %q", errOut.String())
	}

	// 8 days later -> nudges again
	errOut.Reset()
	maybeNudgeConfigMigration(cfg, &errOut, true, home, t0.Add(8*24*time.Hour))
	if !strings.Contains(errOut.String(), "aide config migrate --write") {
		t.Fatalf("expected nudge after 7-day cooldown expired, got %q", errOut.String())
	}
}

func TestSaveConfigWithMigrationPrompt_DeclineOrAccept(t *testing.T) {
	home := isolatedConfigDir(t)
	cfgPath := config.FilePath()
	orig := "profiles:\n  work:\n    agent: claude\n    config_dir_name: work\n"
	if err := os.WriteFile(cfgPath, []byte(orig), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(filepath.Dir(cfgPath), home)
	if err != nil {
		t.Fatal(err)
	}
	cfg.DefaultContext = "work"

	// Decline ("n\n") -> returns ErrLegacyMonolithicConfig and leaves config.yaml untouched
	var errOut bytes.Buffer
	err = saveConfigWithMigrationPromptIO(cfg, cfgPath, home, strings.NewReader("n\n"), &errOut, true)
	if !errors.Is(err, config.ErrLegacyMonolithicConfig) {
		t.Fatalf("expected ErrLegacyMonolithicConfig on decline, got %v", err)
	}
	if got, _ := os.ReadFile(cfgPath); string(got) != orig {
		t.Fatalf("declined migration modified config.yaml:\n%s", got)
	}

	// Accept ("y\n") -> backs up config.yaml.bak and writes split layout with mutation applied
	err = saveConfigWithMigrationPromptIO(cfg, cfgPath, home, strings.NewReader("y\n"), &errOut, true)
	if err != nil {
		t.Fatalf("accept migration failed: %v", err)
	}
	if _, err := os.Stat(cfgPath + ".bak"); err != nil {
		t.Errorf("expected config.yaml.bak: %v", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(cfgPath), "profiles", "work.yaml")); err != nil {
		t.Errorf("expected profiles/work.yaml to be written: %v", err)
	}
}
