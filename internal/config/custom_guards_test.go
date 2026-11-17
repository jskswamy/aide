package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt/guards"
)

func TestLoad_RegistersCustomGuards(t *testing.T) {
	t.Cleanup(func() { _ = guards.SetCustomGuards(nil) })
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "profiles"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "profiles", "work.yaml"), "agent: claude\nconfig_dir_name: work\n")
	writeFile(t, filepath.Join(dir, "sandbox.yaml"), `guard_types:
  team-internal:
    behavior: opt-in
    description: Team credential stores
custom_guards:
  audit-logs:
    type: default
    paths: ["~/.config/audit"]
  team-vault:
    type: team-internal
    paths: ["~/.config/team-vault"]
`)

	if _, err := Load(dir, ""); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if g, ok := guards.GuardByName("audit-logs"); !ok || g.Type() != "default" {
		t.Errorf("audit-logs = %v, %v; want registered default guard", g, ok)
	}
	if g, ok := guards.GuardByName("team-vault"); !ok || g.Type() != "opt-in" {
		t.Errorf("team-vault = %v, %v; want opt-in via guard_types", g, ok)
	}
}

func TestLoad_RejectsInvalidCustomGuards(t *testing.T) {
	t.Cleanup(func() { _ = guards.SetCustomGuards(nil) })
	for name, sandboxYAML := range map[string]string{
		"unknown type":   "custom_guards:\n  x:\n    type: compliance\n    paths: [\"~/.x\"]\n",
		"bad behavior":   "guard_types:\n  compliance:\n    behavior: always\ncustom_guards:\n  x:\n    type: compliance\n    paths: [\"~/.x\"]\n",
		"built-in type":  "guard_types:\n  default:\n    behavior: opt-in\n",
		"built-in guard": "custom_guards:\n  base:\n    paths: [\"~/.x\"]\n",
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, filepath.Join(dir, "sandbox.yaml"), sandboxYAML)
			_, err := Load(dir, "")
			if err == nil || !strings.Contains(err.Error(), "guard") {
				t.Errorf("Load() error = %v, want a guard error", err)
			}
		})
	}
}
