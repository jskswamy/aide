package secrets

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"filippo.io/age"
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
		"aide init",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("error message missing %q:\n%s", want, msg)
		}
	}
}

func TestDiscoverRecipients_SingleAndMultiple(t *testing.T) {
	id1, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	id2, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	id3, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	yubikeyPub := "age1yubikey1qwt50d05yubikeytestrecipient0000000000000000000000000"

	defaultFile := isolateConfigDir(t)
	if err := os.MkdirAll(filepath.Dir(defaultFile), 0o700); err != nil {
		t.Fatal(err)
	}
	// Default file has id3 and a duplicate of id2.
	defaultContent := strings.Join([]string{
		"# default key",
		"# public key: " + id3.Recipient().String(),
		id3.String(),
		"",
		"# duplicate of id2",
		id2.String(),
	}, "\n") + "\n"
	if err := os.WriteFile(defaultFile, []byte(defaultContent), 0o600); err != nil {
		t.Fatal(err)
	}

	// SOPS_AGE_KEY_FILE has id2 and a YubiKey plugin recipient comment.
	envKeyFile := filepath.Join(t.TempDir(), "env-keys.txt")
	envFileContent := strings.Join([]string{
		"# created: 2026-01-15T10:00:00Z",
		"# public key: " + id2.Recipient().String(),
		id2.String(),
		"",
		"# YubiKey slot 1",
		"# Recipient: " + yubikeyPub,
		"AGE-PLUGIN-YUBIKEY-1FAKEPLUGINIDENTITY000000000000",
	}, "\n") + "\n"
	if err := os.WriteFile(envKeyFile, []byte(envFileContent), 0o600); err != nil {
		t.Fatal(err)
	}

	// SOPS_AGE_KEY has leading comment lines + id1.
	envKey := "# env key comment\n# public key: " + id1.Recipient().String() + "\n" + id1.String() + "\n"
	t.Setenv("PATH", "")
	t.Setenv("SOPS_AGE_KEY", envKey)
	t.Setenv("SOPS_AGE_KEY_FILE", envKeyFile)

	// Also verify DiscoverAgeKey accepts SOPS_AGE_KEY with leading comments.
	discoveredID, err := DiscoverAgeKey()
	if err != nil {
		t.Fatalf("DiscoverAgeKey with comment header failed: %v", err)
	}
	if discoveredID.Source != SourceEnvKey {
		t.Errorf("DiscoverAgeKey source = %v, want SourceEnvKey", discoveredID.Source)
	}
	if gotRec, err := discoveredID.Recipient(); err != nil || gotRec != id1.Recipient().String() {
		t.Errorf("discoveredID.Recipient() = %q, %v; want %q", gotRec, err, id1.Recipient().String())
	}

	recs, err := DiscoverRecipients()
	if err != nil {
		t.Fatalf("DiscoverRecipients() error = %v", err)
	}
	if len(recs) != 4 {
		t.Fatalf("DiscoverRecipients() returned %d recipients, want 4: %+v", len(recs), recs)
	}

	wantPubs := []string{
		id1.Recipient().String(),
		id2.Recipient().String(),
		yubikeyPub,
		id3.Recipient().String(),
	}
	for i, want := range wantPubs {
		if recs[i].PublicKey != want {
			t.Errorf("recs[%d].PublicKey = %q, want %q", i, recs[i].PublicKey, want)
		}
	}
	if recs[0].Source != "env:SOPS_AGE_KEY" || recs[0].Label != "env key comment" {
		t.Errorf("recs[0] = %+v, want source env:SOPS_AGE_KEY and label 'env key comment'", recs[0])
	}
	if recs[1].Source != envKeyFile || recs[1].Label != "created: 2026-01-15T10:00:00Z" {
		t.Errorf("recs[1] = %+v, want source %q and label 'created: 2026-01-15T10:00:00Z'", recs[1], envKeyFile)
	}
	if recs[2].Source != envKeyFile || recs[2].Label != "YubiKey slot 1" {
		t.Errorf("recs[2] = %+v, want source %q and label 'YubiKey slot 1'", recs[2], envKeyFile)
	}
	if recs[3].Source != defaultFile || recs[3].Label != "default key" {
		t.Errorf("recs[3] = %+v, want source %q and label 'default key'", recs[3], defaultFile)
	}
}

func TestDiscoverRecipients_UnreadableEnvKeyFileErrors(t *testing.T) {
	isolateConfigDir(t)
	t.Setenv("SOPS_AGE_KEY", "")
	t.Setenv("SOPS_AGE_KEY_FILE", filepath.Join(t.TempDir(), "missing.txt"))

	if _, err := DiscoverRecipients(); err == nil {
		t.Fatal("expected error when SOPS_AGE_KEY_FILE points to a missing file")
	}
}

func TestGenerateDefaultAgeKey(t *testing.T) {
	defaultFile := isolateConfigDir(t)
	t.Setenv("PATH", "")
	t.Setenv("SOPS_AGE_KEY", "")
	t.Setenv("SOPS_AGE_KEY_FILE", "")

	// Generate initial key at default path (passing "" uses defaultKeyPath()).
	rec1, err := GenerateDefaultAgeKey("")
	if err != nil {
		t.Fatalf("GenerateDefaultAgeKey(\"\") error = %v", err)
	}
	if rec1.Source != defaultFile {
		t.Errorf("rec1.Source = %q, want %q", rec1.Source, defaultFile)
	}
	if rec1.Label != "generated" {
		t.Errorf("rec1.Label = %q, want \"generated\"", rec1.Label)
	}
	if err := ValidateRecipient(rec1.PublicKey); err != nil {
		t.Errorf("ValidateRecipient(rec1.PublicKey) error = %v", err)
	}

	info, err := os.Stat(defaultFile)
	if err != nil {
		t.Fatalf("stat generated key file: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("generated key file mode = %o, want 0600", info.Mode().Perm())
	}

	// Generate a second key into the same file and verify it appends cleanly.
	rec2, err := GenerateDefaultAgeKey(defaultFile)
	if err != nil {
		t.Fatalf("second GenerateDefaultAgeKey error = %v", err)
	}
	if rec2.PublicKey == rec1.PublicKey {
		t.Errorf("expected distinct generated public keys, both were %q", rec1.PublicKey)
	}

	recs, err := DiscoverRecipients()
	if err != nil {
		t.Fatalf("DiscoverRecipients() error = %v", err)
	}
	if len(recs) != 2 || recs[0].PublicKey != rec1.PublicKey || recs[1].PublicKey != rec2.PublicKey {
		t.Fatalf("DiscoverRecipients() = %+v, want [%s, %s]", recs, rec1.PublicKey, rec2.PublicKey)
	}

	// Verify round-trip encryption and decryption with the generated key.
	id, err := DiscoverAgeKey()
	if err != nil {
		t.Fatalf("DiscoverAgeKey() error = %v", err)
	}
	secretsDir := filepath.Join(t.TempDir(), "secrets")
	mgr := NewManager(secretsDir, t.TempDir())
	if err := mgr.SetKeys("test", secretsDir, map[string]string{"token": "secret-val"}, rec1.PublicKey); err != nil {
		t.Fatalf("SetKeys() error = %v", err)
	}
	got, err := DecryptSecretsFile(filepath.Join(secretsDir, "test.enc.yaml"), id)
	if err != nil {
		t.Fatalf("DecryptSecretsFile() error = %v", err)
	}
	if got["token"] != "secret-val" {
		t.Errorf("decrypted token = %q, want \"secret-val\"", got["token"])
	}
}

func TestValidateRecipient(t *testing.T) {
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		pubKey  string
		wantErr bool
	}{
		{"valid x25519", id.Recipient().String(), false},
		{"valid x25519 with whitespace", "  " + id.Recipient().String() + "\n", false},
		{"valid yubikey plugin", "age1yubikey1qwt50d05yubikeytestrecipient0000000000000000000000000", false},
		{"empty", "", true},
		{"whitespace only", "   ", true},
		{"wrong prefix", "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5", true},
		{"malformed standard age1", "age1invalid", true},
		{"contains internal whitespace", "age1yubikey1abc def", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRecipient(tt.pubKey)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateRecipient(%q) error = %v, wantErr %v", tt.pubKey, err, tt.wantErr)
			}
		})
	}
}

func TestResolveRecipient(t *testing.T) {
	id1, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	id2, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	customID, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}

	t.Run("explicit valid key", func(t *testing.T) {
		isolateConfigDir(t)
		t.Setenv("SOPS_AGE_KEY", "")
		t.Setenv("SOPS_AGE_KEY_FILE", "")

		var out bytes.Buffer
		got, err := ResolveRecipient("  "+id1.Recipient().String()+" ", nil, &out, false)
		if err != nil {
			t.Fatalf("ResolveRecipient() error = %v", err)
		}
		if got != id1.Recipient().String() {
			t.Errorf("got %q, want %q", got, id1.Recipient().String())
		}
	})

	t.Run("explicit invalid key", func(t *testing.T) {
		isolateConfigDir(t)
		if _, err := ResolveRecipient("not-an-age-key", nil, nil, false); err == nil {
			t.Fatal("expected error for invalid explicit key")
		}
	})

	t.Run("1 key auto-selected", func(t *testing.T) {
		isolateConfigDir(t)
		t.Setenv("SOPS_AGE_KEY", id1.String())
		t.Setenv("SOPS_AGE_KEY_FILE", "")

		var out bytes.Buffer
		got, err := ResolveRecipient("", nil, &out, true)
		if err != nil {
			t.Fatalf("ResolveRecipient() error = %v", err)
		}
		if got != id1.Recipient().String() {
			t.Errorf("got %q, want %q", got, id1.Recipient().String())
		}
		if !strings.Contains(out.String(), "Using age key: "+id1.Recipient().String()+" (env:SOPS_AGE_KEY)") {
			t.Errorf("unexpected output: %q", out.String())
		}
	})

	t.Run("multi-key non-TTY selects first", func(t *testing.T) {
		isolateConfigDir(t)
		t.Setenv("SOPS_AGE_KEY", id1.String()+"\n"+id2.String()+"\n")
		t.Setenv("SOPS_AGE_KEY_FILE", "")

		var out bytes.Buffer
		got, err := ResolveRecipient("", bufio.NewReader(strings.NewReader("2\n")), &out, false)
		if err != nil {
			t.Fatalf("ResolveRecipient() error = %v", err)
		}
		if got != id1.Recipient().String() {
			t.Errorf("got %q, want %q", got, id1.Recipient().String())
		}
	})

	t.Run("multi-key TTY selects [2]", func(t *testing.T) {
		isolateConfigDir(t)
		content := "# first key\n" + id1.String() + "\n# second key\n" + id2.String() + "\n"
		t.Setenv("SOPS_AGE_KEY", content)
		t.Setenv("SOPS_AGE_KEY_FILE", "")

		var out bytes.Buffer
		got, err := ResolveRecipient("", bufio.NewReader(strings.NewReader("2\n")), &out, true)
		if err != nil {
			t.Fatalf("ResolveRecipient() error = %v", err)
		}
		if got != id2.Recipient().String() {
			t.Errorf("got %q, want %q", got, id2.Recipient().String())
		}
		if !strings.Contains(out.String(), "Multiple age keys found:") || !strings.Contains(out.String(), "second key") {
			t.Errorf("expected multi-key menu with labels, got:\n%s", out.String())
		}
	})

	t.Run("multi-key TTY default Enter selects [1]", func(t *testing.T) {
		isolateConfigDir(t)
		t.Setenv("SOPS_AGE_KEY", id1.String()+"\n"+id2.String()+"\n")
		t.Setenv("SOPS_AGE_KEY_FILE", "")

		var out bytes.Buffer
		got, err := ResolveRecipient("", bufio.NewReader(strings.NewReader("\n")), &out, true)
		if err != nil {
			t.Fatalf("ResolveRecipient() error = %v", err)
		}
		if got != id1.Recipient().String() {
			t.Errorf("got %q, want %q", got, id1.Recipient().String())
		}
	})

	t.Run("multi-key TTY selects [N+1] custom key", func(t *testing.T) {
		isolateConfigDir(t)
		t.Setenv("SOPS_AGE_KEY", id1.String()+"\n"+id2.String()+"\n")
		t.Setenv("SOPS_AGE_KEY_FILE", "")

		var out bytes.Buffer
		in := bufio.NewReader(strings.NewReader("3\n" + customID.Recipient().String() + "\n"))
		got, err := ResolveRecipient("", in, &out, true)
		if err != nil {
			t.Fatalf("ResolveRecipient() error = %v", err)
		}
		if got != customID.Recipient().String() {
			t.Errorf("got %q, want %q", got, customID.Recipient().String())
		}
	})

	t.Run("multi-key TTY invalid selection", func(t *testing.T) {
		isolateConfigDir(t)
		t.Setenv("SOPS_AGE_KEY", id1.String()+"\n"+id2.String()+"\n")
		t.Setenv("SOPS_AGE_KEY_FILE", "")

		var out bytes.Buffer
		_, err := ResolveRecipient("", bufio.NewReader(strings.NewReader("99\n")), &out, true)
		if err == nil || !strings.Contains(err.Error(), "invalid selection") {
			t.Fatalf("expected invalid selection error, got: %v", err)
		}
	})

	t.Run("0-key TTY auto-generates on Enter", func(t *testing.T) {
		defaultFile := isolateConfigDir(t)
		t.Setenv("PATH", "")
		t.Setenv("SOPS_AGE_KEY", "")
		t.Setenv("SOPS_AGE_KEY_FILE", "")

		var out bytes.Buffer
		got, err := ResolveRecipient("", bufio.NewReader(strings.NewReader("\n")), &out, true)
		if err != nil {
			t.Fatalf("ResolveRecipient() error = %v", err)
		}
		if err := ValidateRecipient(got); err != nil {
			t.Errorf("generated recipient invalid: %v", err)
		}
		if _, err := os.Stat(defaultFile); err != nil {
			t.Errorf("expected generated key file at %s: %v", defaultFile, err)
		}
		if !strings.Contains(out.String(), "✓ Generated age key at "+defaultFile) {
			t.Errorf("unexpected output:\n%s", out.String())
		}
	})

	t.Run("0-key TTY custom public key [2]", func(t *testing.T) {
		isolateConfigDir(t)
		t.Setenv("PATH", "")
		t.Setenv("SOPS_AGE_KEY", "")
		t.Setenv("SOPS_AGE_KEY_FILE", "")

		var out bytes.Buffer
		in := bufio.NewReader(strings.NewReader("2\n" + customID.Recipient().String() + "\n"))
		got, err := ResolveRecipient("", in, &out, true)
		if err != nil {
			t.Fatalf("ResolveRecipient() error = %v", err)
		}
		if got != customID.Recipient().String() {
			t.Errorf("got %q, want %q", got, customID.Recipient().String())
		}
	})

	t.Run("0-key TTY invalid selection", func(t *testing.T) {
		isolateConfigDir(t)
		t.Setenv("PATH", "")
		t.Setenv("SOPS_AGE_KEY", "")
		t.Setenv("SOPS_AGE_KEY_FILE", "")

		var out bytes.Buffer
		_, err := ResolveRecipient("", bufio.NewReader(strings.NewReader("9\n")), &out, true)
		if err == nil || !strings.Contains(err.Error(), "invalid selection") {
			t.Fatalf("expected invalid selection error, got: %v", err)
		}
	})

	t.Run("0-key non-TTY returns DiscoverAgeKey error", func(t *testing.T) {
		isolateConfigDir(t)
		t.Setenv("PATH", "")
		t.Setenv("SOPS_AGE_KEY", "")
		t.Setenv("SOPS_AGE_KEY_FILE", "")

		_, err := ResolveRecipient("", nil, nil, false)
		if err == nil || !strings.Contains(err.Error(), "no age identity found") {
			t.Fatalf("expected no age identity found error, got: %v", err)
		}
	})
}
