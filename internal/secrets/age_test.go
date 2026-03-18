package secrets

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

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
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // empty config dir

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
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

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
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	_, err := DiscoverAgeKey()
	if err == nil {
		t.Fatal("expected error when no valid key is found, got nil")
	}
	// Should have fallen through all sources and returned an error.
	if !strings.Contains(err.Error(), "No age identity found") {
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
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

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
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	_, err := DiscoverAgeKey()
	if err == nil {
		t.Fatal("expected error when key file is missing, got nil")
	}
	if !strings.Contains(err.Error(), "No age identity found") {
		t.Errorf("expected 'No age identity found' in error, got: %v", err)
	}
}

func TestDiscoverAgeKey_DefaultPath(t *testing.T) {
	xdgDir := t.TempDir()
	keyDir := filepath.Join(xdgDir, "sops", "age")
	if err := os.MkdirAll(keyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	keyFile := filepath.Join(keyDir, "keys.txt")
	if err := os.WriteFile(keyFile, []byte("AGE-SECRET-KEY-1DEFAULT\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", "")
	t.Setenv("SOPS_AGE_KEY", "")
	t.Setenv("SOPS_AGE_KEY_FILE", "")
	t.Setenv("XDG_CONFIG_HOME", xdgDir)

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

	xdgDir := t.TempDir()
	keyDir := filepath.Join(xdgDir, "sops", "age")
	if err := os.MkdirAll(keyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(keyDir, "keys.txt"), []byte("AGE-SECRET-KEY-1\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("SOPS_AGE_KEY", "AGE-SECRET-KEY-1FAKE0000000000000000000000000000000000000000000000000000000")
	t.Setenv("SOPS_AGE_KEY_FILE", "")
	t.Setenv("XDG_CONFIG_HOME", xdgDir)

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

func TestDiscoverAgeKey_NoneFound(t *testing.T) {
	t.Setenv("PATH", "")
	t.Setenv("SOPS_AGE_KEY", "")
	t.Setenv("SOPS_AGE_KEY_FILE", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // empty

	_, err := DiscoverAgeKey()
	if err == nil {
		t.Fatal("expected error when nothing is found")
	}

	msg := err.Error()
	// Check the error contains actionable guidance.
	for _, want := range []string{
		"No age identity found",
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
