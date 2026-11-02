package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoad_SplitLayout(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "config.yaml"), "default_profile: work\nagents:\n  claude:\n    binary: claude\n")
	writeFile(t, filepath.Join(dir, "sandbox.yaml"), "never_allow:\n  - ~/.kube/prod\ncapabilities:\n  bazel:\n    writable: [~/.cache/bazel]\n")
	writeFile(t, filepath.Join(dir, "provisioning.yaml"), "plugins:\n  steveyegge/beads: [beads]\n")
	if err := os.MkdirAll(filepath.Join(dir, "profiles"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "profiles", "work.yaml"), "agent: claude\nconfig_dir_name: work\ncapabilities: [bazel]\n")
	writeFile(t, filepath.Join(dir, "profiles", "work.yaml.bak"), "agent: ignored\n")
	writeFile(t, filepath.Join(dir, "profiles", ".hidden.yaml"), "agent: ignored\n")

	cfg, err := Load(dir, "")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.LegacyMonolithic {
		t.Error("expected LegacyMonolithic = false for split layout")
	}
	if !cfg.ProfileSchema {
		t.Error("expected ProfileSchema = true for split layout")
	}
	if cfg.DefaultContext != "work" {
		t.Errorf("DefaultContext = %q, want work", cfg.DefaultContext)
	}
	if len(cfg.Contexts) != 1 || cfg.Contexts["work"].Profile != "work" {
		t.Errorf("Contexts = %+v, want work profile with Profile=work", cfg.Contexts)
	}
	if len(cfg.Capabilities) != 1 || len(cfg.NeverAllow) != 1 {
		t.Errorf("sandbox.yaml not merged: caps=%v never_allow=%v", cfg.Capabilities, cfg.NeverAllow)
	}
	if len(cfg.Plugins) != 1 {
		t.Errorf("provisioning.yaml not merged: plugins=%v", cfg.Plugins)
	}
}

func TestLoad_SplitLayoutWithoutConfigYAML(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "profiles"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "profiles", "work.yaml"), "agent: claude\nconfig_dir_name: work\n")

	cfg, err := Load(dir, "")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.LegacyMonolithic || !cfg.ProfileSchema {
		t.Errorf("LegacyMonolithic=%v ProfileSchema=%v, want false/true", cfg.LegacyMonolithic, cfg.ProfileSchema)
	}
	if cfg.Contexts["work"].Agent != "claude" {
		t.Errorf("work agent = %q, want claude", cfg.Contexts["work"].Agent)
	}
}

func TestLoad_DetectsLegacyMonolithic(t *testing.T) {
	cases := map[string]string{
		"inline profiles":     "profiles:\n  work:\n    agent: claude\n",
		"inline contexts":     "contexts:\n  work:\n    agent: claude\n",
		"minimal flat":        "agent: claude\nsecret: personal\n",
		"inline capabilities": "default_profile: work\ncapabilities:\n  foo:\n    readable: [~/a]\n",
		"inline plugins":      "default_profile: work\nplugins:\n  a/b: [c]\n",
		"default_context key": "default_context: work\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, filepath.Join(dir, "config.yaml"), body)
			cfg, err := Load(dir, "")
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if !cfg.LegacyMonolithic {
				t.Errorf("expected LegacyMonolithic = true for %s", name)
			}
		})
	}
}

func TestLoad_RejectsDuplicatesBetweenConfigAndSplitFiles(t *testing.T) {
	t.Run("duplicate profile", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "config.yaml"), "profiles:\n  work:\n    agent: claude\n")
		if err := os.MkdirAll(filepath.Join(dir, "profiles"), 0o755); err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(dir, "profiles", "work.yaml"), "agent: claude\n")
		_, err := Load(dir, "")
		if !errors.Is(err, ErrDuplicateProfile) {
			t.Fatalf("Load() error = %v, want ErrDuplicateProfile", err)
		}
	})
	t.Run("duplicate sandbox section", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "config.yaml"), "never_allow: [~/.ssh]\n")
		writeFile(t, filepath.Join(dir, "sandbox.yaml"), "never_allow: [~/.aws]\n")
		_, err := Load(dir, "")
		if !errors.Is(err, ErrDuplicateConfigSection) {
			t.Fatalf("Load() error = %v, want ErrDuplicateConfigSection", err)
		}
	})
	t.Run("duplicate provisioning section", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "config.yaml"), "plugins:\n  a/b: [c]\n")
		writeFile(t, filepath.Join(dir, "provisioning.yaml"), "plugins:\n  d/e: [f]\n")
		_, err := Load(dir, "")
		if !errors.Is(err, ErrDuplicateConfigSection) {
			t.Fatalf("Load() error = %v, want ErrDuplicateConfigSection", err)
		}
	})
}

func TestLoad_RejectsMisplacedKeysInSplitFiles(t *testing.T) {
	t.Run("contexts key in profile file", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dir, "profiles"), 0o755); err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(dir, "profiles", "work.yaml"), "agent: claude\nprofile: work\n")
		_, err := Load(dir, "")
		if err == nil || !strings.Contains(err.Error(), "config_dir_name") {
			t.Fatalf("Load() error = %v, want config_dir_name hint", err)
		}
	})
	t.Run("unknown key in sandbox.yaml", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "sandbox.yaml"), "profiles:\n  work:\n    agent: claude\n")
		_, err := Load(dir, "")
		if err == nil {
			t.Fatal("expected error for profiles: inside sandbox.yaml")
		}
	})
	t.Run("unknown key in provisioning.yaml", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "provisioning.yaml"), "never_allow: [~/.ssh]\n")
		_, err := Load(dir, "")
		if err == nil {
			t.Fatal("expected error for never_allow: inside provisioning.yaml")
		}
	})
}
