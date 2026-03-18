package config

import (
	"path/filepath"
	"testing"
)

func TestConfigDirFrom(t *testing.T) {
	base := t.TempDir()
	got := ConfigDirFrom(base)
	want := filepath.Join(base, "aide")
	if got != want {
		t.Errorf("ConfigDirFrom(%q) = %q, want %q", base, got, want)
	}
}

func TestConfigDir_Default(t *testing.T) {
	dir := ConfigDir()
	if !filepath.IsAbs(dir) {
		t.Errorf("ConfigDir() returned non-absolute path: %q", dir)
	}
	if filepath.Base(dir) != "aide" {
		t.Errorf("ConfigDir() should end with 'aide', got %q", dir)
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

func TestConfigFilePathFrom(t *testing.T) {
	base := t.TempDir()
	got := ConfigFilePathFrom(base)
	want := filepath.Join(base, "aide", "config.yaml")
	if got != want {
		t.Errorf("ConfigFilePathFrom(%q) = %q, want %q", base, got, want)
	}
}

func TestResolveSecretsFilePathFrom_Relative(t *testing.T) {
	base := t.TempDir()
	got := ResolveSecretsFilePathFrom(base, "personal.enc.yaml")
	want := filepath.Join(base, "aide", "secrets", "personal.enc.yaml")
	if got != want {
		t.Errorf("ResolveSecretsFilePathFrom(%q, 'personal.enc.yaml') = %q, want %q", base, got, want)
	}
}

func TestResolveSecretsFilePathFrom_Absolute(t *testing.T) {
	got := ResolveSecretsFilePathFrom("/tmp/xdg", "/custom/path/keys.yaml")
	want := "/custom/path/keys.yaml"
	if got != want {
		t.Errorf("ResolveSecretsFilePathFrom should return absolute path as-is, got %q, want %q", got, want)
	}
}
