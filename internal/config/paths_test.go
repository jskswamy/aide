package config

import (
	"path/filepath"
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
