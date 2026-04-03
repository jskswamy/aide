package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestDirFrom(t *testing.T) {
	base := t.TempDir()
	got := DirFrom(base)
	want := filepath.Join(base, "aide")
	if got != want {
		t.Errorf("DirFrom(%q) = %q, want %q", base, got, want)
	}
}

func TestDir_Default(t *testing.T) {
	dir := Dir()
	if !filepath.IsAbs(dir) {
		t.Errorf("Dir() returned non-absolute path: %q", dir)
	}
	if filepath.Base(dir) != "aide" {
		t.Errorf("Dir() should end with 'aide', got %q", dir)
	}
}

func TestSecretsDirFrom(t *testing.T) {
	base := t.TempDir()
	got := SecretsDirFrom(base)
	want := filepath.Join(base, "aide", "secrets")
	if got != want {
		t.Errorf("SecretsDirFrom(%q) = %q, want %q", base, got, want)
	}
}

func TestRuntimeDirFrom_ContainsPID(t *testing.T) {
	got := RuntimeDirFrom("/tmp/run", 12345)
	want := "/tmp/run/aide-12345"
	if got != want {
		t.Errorf("RuntimeDirFrom(/tmp/run, 12345) = %q, want %q", got, want)
	}
}

func TestFilePathFrom(t *testing.T) {
	base := t.TempDir()
	got := FilePathFrom(base)
	want := filepath.Join(base, "aide", "config.yaml")
	if got != want {
		t.Errorf("FilePathFrom(%q) = %q, want %q", base, got, want)
	}
}

func TestResolveSecretPathFrom_Relative(t *testing.T) {
	base := t.TempDir()
	got := ResolveSecretPathFrom(base, "personal.enc.yaml")
	want := filepath.Join(base, "aide", "secrets", "personal.enc.yaml")
	if got != want {
		t.Errorf("ResolveSecretPathFrom(%q, 'personal.enc.yaml') = %q, want %q", base, got, want)
	}
}

func TestResolveSecretPathFrom_BareName(t *testing.T) {
	base := t.TempDir()
	got := ResolveSecretPathFrom(base, "personal")
	want := filepath.Join(base, "aide", "secrets", "personal.enc.yaml")
	if got != want {
		t.Errorf("ResolveSecretPathFrom(%q, 'personal') = %q, want %q", base, got, want)
	}
}

func TestResolveSecretPathFrom_Absolute(t *testing.T) {
	got := ResolveSecretPathFrom("/tmp/xdg", "/custom/path/keys.yaml")
	want := "/custom/path/keys.yaml"
	if got != want {
		t.Errorf("ResolveSecretPathFrom should return absolute path as-is, got %q, want %q", got, want)
	}
}

func TestConfigHome(t *testing.T) {
	t.Run("with XDG_CONFIG_HOME", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", "/custom/config")
		got := configHome()
		if got != "/custom/config" {
			t.Errorf("configHome() = %q, want /custom/config", got)
		}
	})

	t.Run("without XDG_CONFIG_HOME", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", "")
		got := configHome()
		if got == "" {
			t.Error("configHome() returned empty string")
		}
	})
}

func TestConvenienceWrappers(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/custom/config")

	t.Run("SecretsDir", func(t *testing.T) {
		got := SecretsDir()
		if got == "" {
			t.Error("SecretsDir() returned empty string")
		}
	})

	t.Run("FilePath", func(t *testing.T) {
		got := FilePath()
		if got == "" {
			t.Error("FilePath() returned empty string")
		}
	})

	t.Run("ResolveSecretPath absolute", func(t *testing.T) {
		got := ResolveSecretPath("/abs/path.enc.yaml")
		if got != "/abs/path.enc.yaml" {
			t.Errorf("got %q, want /abs/path.enc.yaml", got)
		}
	})

	t.Run("ResolveSecretPath bare name", func(t *testing.T) {
		got := ResolveSecretPath("work")
		if !strings.HasSuffix(got, "work.enc.yaml") {
			t.Errorf("got %q, want suffix work.enc.yaml", got)
		}
	})
}

func TestRuntimeDir(t *testing.T) {
	got := RuntimeDir(12345)
	if got == "" {
		t.Error("RuntimeDir() returned empty string")
	}
	if !strings.Contains(got, "aide-12345") {
		t.Errorf("RuntimeDir() = %q, want it to contain aide-12345", got)
	}
}
