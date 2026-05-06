package secrets

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// isolateConfigDir redirects os.UserConfigDir() and the XDG fallback into a
// temp directory on every OS, then returns the sops/age/keys.txt path that
// DiscoverAgeKey will probe first.
func isolateConfigDir(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	dir, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("UserConfigDir: %v", err)
	}
	return filepath.Join(dir, "sops", "age", "keys.txt")
}

func TestDiscoverAgeKey_YubiKeyOnPath(t *testing.T) {
	// Create a fake age-plugin-yubikey binary on a temp PATH dir.
	tmpDir := t.TempDir()
	fakeBin := filepath.Join(tmpDir, "age-plugin-yubikey")
	if runtime.GOOS == "windows" {
		fakeBin += ".exe"
	}
	if err := os.WriteFile(fakeBin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Clear all other sources so YubiKey is the only option.
	t.Setenv("PATH", tmpDir)
	t.Setenv("SOPS_AGE_KEY", "")
	t.Setenv("SOPS_AGE_KEY_FILE", "")
	isolateConfigDir(t)

	id, err := DiscoverAgeKey()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if id.Source != SourceYubiKey {
		t.Errorf("expected SourceYubiKey, got %v", id.Source)
	}
}

func TestDiscoverAgeKey_EnvKey(t *testing.T) {
	t.Setenv("PATH", "") // no yubikey
	t.Setenv("SOPS_AGE_KEY", "AGE-SECRET-KEY-1FAKE0000000000000000000000000000000000000000000000000000000")
	t.Setenv("SOPS_AGE_KEY_FILE", "")
	isolateConfigDir(t)

	id, err := DiscoverAgeKey()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if id.Source != SourceEnvKey {
		t.Errorf("expected SourceEnvKey, got %v", id.Source)
	}
	if id.KeyData != "AGE-SECRET-KEY-1FAKE0000000000000000000000000000000000000000000000000000000" {
		t.Errorf("unexpected KeyData: %q", id.KeyData)
	}
}

func TestDiscoverAgeKey_EnvKeyInvalid(t *testing.T) {
	// Set SOPS_AGE_KEY to an invalid value; it should be skipped (fall through).
	t.Setenv("PATH", "")
	t.Setenv("SOPS_AGE_KEY", "not-a-valid-key")
	t.Setenv("SOPS_AGE_KEY_FILE", "")
	isolateConfigDir(t)

	_, err := DiscoverAgeKey()
	if err == nil {
		t.Fatal("expected error when no valid key is found, got nil")
	}
	// Should have fallen through all sources and returned an error.
	if !strings.Contains(err.Error(), "no age identity found") {
		t.Errorf("expected 'No age identity found' in error, got: %v", err)
	}
}

func TestDiscoverAgeKey_EnvKeyFile(t *testing.T) {
	tmpDir := t.TempDir()
	keyFile := filepath.Join(tmpDir, "keys.txt")
	if err := os.WriteFile(keyFile, []byte("AGE-SECRET-KEY-1FAKE\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", "")
	t.Setenv("SOPS_AGE_KEY", "")
	t.Setenv("SOPS_AGE_KEY_FILE", keyFile)
	isolateConfigDir(t)

	id, err := DiscoverAgeKey()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if id.Source != SourceEnvKeyFile {
		t.Errorf("expected SourceEnvKeyFile, got %v", id.Source)
	}
	if id.KeyData != keyFile {
		t.Errorf("expected KeyData=%q, got %q", keyFile, id.KeyData)
	}
}

func TestDiscoverAgeKey_EnvKeyFileMissing(t *testing.T) {
	t.Setenv("PATH", "")
	t.Setenv("SOPS_AGE_KEY", "")
	t.Setenv("SOPS_AGE_KEY_FILE", "/nonexistent/keys.txt")
	isolateConfigDir(t)

	_, err := DiscoverAgeKey()
	if err == nil {
		t.Fatal("expected error when key file is missing, got nil")
	}
	if !strings.Contains(err.Error(), "no age identity found") {
		t.Errorf("expected 'No age identity found' in error, got: %v", err)
	}
}

func TestDiscoverAgeKey_DefaultPath(t *testing.T) {
	keyFile := isolateConfigDir(t)
	if err := os.MkdirAll(filepath.Dir(keyFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, []byte("AGE-SECRET-KEY-1DEFAULT\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", "")
	t.Setenv("SOPS_AGE_KEY", "")
	t.Setenv("SOPS_AGE_KEY_FILE", "")

	id, err := DiscoverAgeKey()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if id.Source != SourceDefaultFile {
		t.Errorf("expected SourceDefaultFile, got %v", id.Source)
	}
	if id.KeyData != keyFile {
		t.Errorf("expected KeyData=%q, got %q", keyFile, id.KeyData)
	}
}

func TestDiscoverAgeKey_DefaultPathXDGFallbackOnDarwin(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("XDG fallback path only applies on macOS")
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	// Place key only at the XDG-style ~/.config path, not at the macOS
	// canonical ~/Library/Application Support path.
	xdgKey := filepath.Join(home, ".config", "sops", "age", "keys.txt")
	if err := os.MkdirAll(filepath.Dir(xdgKey), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(xdgKey, []byte("AGE-SECRET-KEY-1XDG\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", "")
	t.Setenv("SOPS_AGE_KEY", "")
	t.Setenv("SOPS_AGE_KEY_FILE", "")

	id, err := DiscoverAgeKey()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if id.Source != SourceDefaultFile {
		t.Errorf("expected SourceDefaultFile, got %v", id.Source)
	}
	if id.KeyData != xdgKey {
		t.Errorf("expected KeyData=%q, got %q", xdgKey, id.KeyData)
	}
}

// TestDiscoverAgeKey_MacOSLibraryPath_Regression pins the fix for a bug where
// DiscoverAgeKey only checked $XDG_CONFIG_HOME/sops/age/keys.txt and missed
// the sops/age canonical path on macOS, ~/Library/Application Support/sops/age/keys.txt.
// Symptom: aide users on macOS without YubiKey or env vars hit
// "no age identity found" even though sops's own default key was present.
func TestDiscoverAgeKey_MacOSLibraryPath_Regression(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("regression specific to macOS path resolution")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")

	keyFile := filepath.Join(home, "Library", "Application Support", "sops", "age", "keys.txt")
	if err := os.MkdirAll(filepath.Dir(keyFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, []byte("AGE-SECRET-KEY-1MAC\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", "")
	t.Setenv("SOPS_AGE_KEY", "")
	t.Setenv("SOPS_AGE_KEY_FILE", "")

	id, err := DiscoverAgeKey()
	if err != nil {
		t.Fatalf("expected discovery to succeed for key at macOS canonical path, got: %v", err)
	}
	if id.Source != SourceDefaultFile {
		t.Errorf("expected SourceDefaultFile, got %v", id.Source)
	}
	if id.KeyData != keyFile {
		t.Errorf("expected KeyData=%q, got %q", keyFile, id.KeyData)
	}
}

func TestDiscoverAgeKey_PriorityOrder(t *testing.T) {
	// Set up all sources: YubiKey, env key, default file.
	tmpBin := t.TempDir()
	fakeBin := filepath.Join(tmpBin, "age-plugin-yubikey")
	if runtime.GOOS == "windows" {
		fakeBin += ".exe"
	}
	if err := os.WriteFile(fakeBin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	keyFile := isolateConfigDir(t)
	if err := os.MkdirAll(filepath.Dir(keyFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, []byte("AGE-SECRET-KEY-1\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("SOPS_AGE_KEY", "AGE-SECRET-KEY-1FAKE0000000000000000000000000000000000000000000000000000000")
	t.Setenv("SOPS_AGE_KEY_FILE", "")

	// With YubiKey on PATH, it should win.
	t.Setenv("PATH", tmpBin)
	id, err := DiscoverAgeKey()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if id.Source != SourceYubiKey {
		t.Errorf("expected SourceYubiKey (highest priority), got %v", id.Source)
	}

	// Remove YubiKey from PATH; env key should win.
	t.Setenv("PATH", "")
	id, err = DiscoverAgeKey()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if id.Source != SourceEnvKey {
		t.Errorf("expected SourceEnvKey, got %v", id.Source)
	}
}

func TestFileReadable(t *testing.T) {
	t.Run("regular file", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "test.txt")
		if err := os.WriteFile(f, []byte("data"), 0o600); err != nil {
			t.Fatal(err)
		}
		if !fileReadable(f) {
			t.Error("fileReadable() = false for existing regular file")
		}
	})

	t.Run("directory", func(t *testing.T) {
		d := t.TempDir()
		if fileReadable(d) {
			t.Error("fileReadable() = true for directory")
		}
	})

	t.Run("missing", func(t *testing.T) {
		if fileReadable("/nonexistent/path/file.txt") {
			t.Error("fileReadable() = true for missing file")
		}
	})
}

func TestDefaultKeyPath(t *testing.T) {
	keyFile := isolateConfigDir(t)
	got := defaultKeyPath()
	if got != keyFile {
		t.Errorf("defaultKeyPath() = %q, want %q", got, keyFile)
	}

	switch runtime.GOOS {
	case "darwin":
		if !strings.Contains(got, "Library/Application Support/sops/age/keys.txt") {
			t.Errorf("on darwin, expected Library/Application Support path, got %q", got)
		}
	case "linux":
		if !strings.HasSuffix(got, ".config/sops/age/keys.txt") {
			t.Errorf("on linux, expected .config/sops/age/keys.txt suffix, got %q", got)
		}
	}
}

func TestDefaultKeyPaths_DarwinIncludesXDGFallback(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("XDG fallback only added on macOS")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	paths := defaultKeyPaths()
	if len(paths) < 2 {
		t.Fatalf("expected >=2 paths on darwin, got %v", paths)
	}
	xdg := filepath.Join(home, ".config", "sops", "age", "keys.txt")
	if !slices.Contains(paths, xdg) {
		t.Errorf("expected %q in paths, got %v", xdg, paths)
	}
}

func TestDiscoverAgeKey_NoneFound(t *testing.T) {
	t.Setenv("PATH", "")
	t.Setenv("SOPS_AGE_KEY", "")
	t.Setenv("SOPS_AGE_KEY_FILE", "")
	isolateConfigDir(t)

	_, err := DiscoverAgeKey()
	if err == nil {
		t.Fatal("expected error when nothing is found")
	}

	msg := err.Error()
	// Check the error contains actionable guidance.
	for _, want := range []string{
		"no age identity found",
		"SOPS_AGE_KEY",
		"SOPS_AGE_KEY_FILE",
		"age-keygen",
		"aide setup",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("error message missing %q:\n%s", want, msg)
		}
	}
}
