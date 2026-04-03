package secrets

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// testdataDir returns the absolute path to the repo-root testdata/ directory.
func testdataDir(t *testing.T) string {
	t.Helper()
	// This file is at internal/secrets/sops_test.go.
	// The testdata directory is at the repo root: ../../testdata/
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("unable to determine test file path")
	}
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "testdata")
}

func TestDecryptSecretsFile_Success(t *testing.T) {
	td := testdataDir(t)
	keyFile := filepath.Join(td, "age-key.txt")
	encFile := filepath.Join(td, "test-secrets.enc.yaml")

	identity := &AgeIdentity{
		Source:  SourceEnvKeyFile,
		KeyData: keyFile,
	}

	secrets, err := DecryptSecretsFile(encFile, identity)
	if err != nil {
		t.Fatalf("DecryptSecretsFile failed: %v", err)
	}

	expectedKeys := []string{"anthropic_api_key", "openai_api_key", "context7_token"}
	for _, key := range expectedKeys {
		if _, ok := secrets[key]; !ok {
			t.Errorf("expected key %q in decrypted secrets, got keys: %v", key, mapKeys(secrets))
		}
	}

	// Verify values are non-empty strings.
	for k, v := range secrets {
		if v == "" {
			t.Errorf("expected non-empty value for key %q", k)
		}
	}
}

func TestDecryptSecretsFile_RelativePath(t *testing.T) {
	// Set up a fake XDG_CONFIG_HOME with aide/secrets/ containing the encrypted file.
	td := testdataDir(t)
	keyFile := filepath.Join(td, "age-key.txt")

	xdgDir := t.TempDir()
	secretsDir := filepath.Join(xdgDir, "aide", "secrets")
	if err := os.MkdirAll(secretsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Copy the encrypted file into the secrets dir.
	encData, err := os.ReadFile(filepath.Join(td, "test-secrets.enc.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(secretsDir, "test-secrets.enc.yaml"), encData, 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("XDG_CONFIG_HOME", xdgDir)

	identity := &AgeIdentity{
		Source:  SourceEnvKeyFile,
		KeyData: keyFile,
	}

	secrets, err := DecryptSecretsFile("test-secrets.enc.yaml", identity)
	if err != nil {
		t.Fatalf("DecryptSecretsFile with relative path failed: %v", err)
	}

	if _, ok := secrets["anthropic_api_key"]; !ok {
		t.Errorf("expected anthropic_api_key in secrets, got: %v", mapKeys(secrets))
	}
}

func TestDecryptSecretsFile_AbsolutePath(t *testing.T) {
	td := testdataDir(t)
	keyFile := filepath.Join(td, "age-key.txt")
	encFile := filepath.Join(td, "test-secrets.enc.yaml")

	identity := &AgeIdentity{
		Source:  SourceEnvKeyFile,
		KeyData: keyFile,
	}

	secrets, err := DecryptSecretsFile(encFile, identity)
	if err != nil {
		t.Fatalf("DecryptSecretsFile with absolute path failed: %v", err)
	}

	if len(secrets) == 0 {
		t.Error("expected non-empty secrets map")
	}
}

func TestDecryptSecretsFile_FileNotFound(t *testing.T) {
	identity := &AgeIdentity{
		Source:  SourceEnvKey,
		KeyData: "AGE-SECRET-KEY-1FAKE",
	}

	_, err := DecryptSecretsFile("/nonexistent/secrets.enc.yaml", identity)
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
	if !strings.Contains(err.Error(), "/nonexistent/secrets.enc.yaml") {
		t.Errorf("error should contain file path, got: %v", err)
	}
}

func TestDecryptSecretsFile_WrongKey(t *testing.T) {
	td := testdataDir(t)
	encFile := filepath.Join(td, "test-secrets.enc.yaml")

	// Use a different age key that was not used to encrypt the file.
	identity := &AgeIdentity{
		Source:  SourceEnvKey,
		KeyData: "AGE-SECRET-KEY-1QQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQ",
	}

	_, err := DecryptSecretsFile(encFile, identity)
	if err == nil {
		t.Fatal("expected error when decrypting with wrong key, got nil")
	}
}

func TestDecryptSecretsFile_NonStringValue(t *testing.T) {
	td := testdataDir(t)
	keyFile := filepath.Join(td, "age-key.txt")

	// Create a plain (non-sops) YAML file with nested values to simulate
	// what we'd get after decryption. We test the unmarshal validation
	// by creating a temp file that sops would return as having nested values.
	// Since we can't easily create a sops-encrypted file with nested values in a test,
	// we test the unmarshal path directly via a helper if possible.
	// For now, verify that passing a non-sops file returns an error.
	tmpDir := t.TempDir()
	nestedFile := filepath.Join(tmpDir, "nested.enc.yaml")
	if err := os.WriteFile(nestedFile, []byte("key:\n  nested: value\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	identity := &AgeIdentity{
		Source:  SourceEnvKeyFile,
		KeyData: keyFile,
	}

	_, err := DecryptSecretsFile(nestedFile, identity)
	if err == nil {
		t.Fatal("expected error for non-sops YAML file, got nil")
	}
}

func TestDecryptSecretsFile_InvalidFile(t *testing.T) {
	td := testdataDir(t)
	keyFile := filepath.Join(td, "age-key.txt")

	// Create a corrupted file.
	tmpDir := t.TempDir()
	badFile := filepath.Join(tmpDir, "bad.enc.yaml")
	if err := os.WriteFile(badFile, []byte("this is not valid sops yaml at all!!!"), 0o600); err != nil {
		t.Fatal(err)
	}

	identity := &AgeIdentity{
		Source:  SourceEnvKeyFile,
		KeyData: keyFile,
	}

	_, err := DecryptSecretsFile(badFile, identity)
	if err == nil {
		t.Fatal("expected error for corrupted file, got nil")
	}
}

// mapKeys returns the keys of a map for diagnostic output.
func mapKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func TestResolveSecretsPath(t *testing.T) {
	t.Run("absolute path", func(t *testing.T) {
		got := resolveSecretsPath("/absolute/path/secrets.enc.yaml")
		if got != "/absolute/path/secrets.enc.yaml" {
			t.Errorf("resolveSecretsPath() = %q, want absolute path unchanged", got)
		}
	})

	t.Run("relative path with XDG", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", "/custom/config")
		got := resolveSecretsPath("work.enc.yaml")
		want := "/custom/config/aide/secrets/work.enc.yaml"
		if got != want {
			t.Errorf("resolveSecretsPath() = %q, want %q", got, want)
		}
	})

	t.Run("relative path without XDG", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", "")
		got := resolveSecretsPath("personal.enc.yaml")
		if !strings.HasSuffix(got, ".config/aide/secrets/personal.enc.yaml") {
			t.Errorf("resolveSecretsPath() = %q, want suffix .config/aide/secrets/personal.enc.yaml", got)
		}
	})
}

func TestSetupDecryptEnv(t *testing.T) {
	t.Run("yubikey source", func(t *testing.T) {
		cleanup, err := setupDecryptEnv(&AgeIdentity{Source: SourceYubiKey})
		if err != nil {
			t.Fatalf("setupDecryptEnv() error = %v", err)
		}
		cleanup()
	})

	t.Run("env key source", func(t *testing.T) {
		t.Setenv("SOPS_AGE_KEY", "original")
		t.Setenv("SOPS_AGE_KEY_FILE", "original-file")

		cleanup, err := setupDecryptEnv(&AgeIdentity{
			Source:  SourceEnvKey,
			KeyData: "AGE-SECRET-KEY-1TEST",
		})
		if err != nil {
			t.Fatalf("setupDecryptEnv() error = %v", err)
		}

		if got := os.Getenv("SOPS_AGE_KEY"); got != "AGE-SECRET-KEY-1TEST" {
			t.Errorf("SOPS_AGE_KEY = %q, want AGE-SECRET-KEY-1TEST", got)
		}
		if got := os.Getenv("SOPS_AGE_KEY_FILE"); got != "" {
			t.Errorf("SOPS_AGE_KEY_FILE = %q, want empty", got)
		}

		cleanup()
	})

	t.Run("key file source", func(t *testing.T) {
		t.Setenv("SOPS_AGE_KEY", "original")
		t.Setenv("SOPS_AGE_KEY_FILE", "original-file")

		cleanup, err := setupDecryptEnv(&AgeIdentity{
			Source:  SourceEnvKeyFile,
			KeyData: "/path/to/key.txt",
		})
		if err != nil {
			t.Fatalf("setupDecryptEnv() error = %v", err)
		}

		if got := os.Getenv("SOPS_AGE_KEY_FILE"); got != "/path/to/key.txt" {
			t.Errorf("SOPS_AGE_KEY_FILE = %q, want /path/to/key.txt", got)
		}
		if got := os.Getenv("SOPS_AGE_KEY"); got != "" {
			t.Errorf("SOPS_AGE_KEY = %q, want empty", got)
		}

		cleanup()
	})

	t.Run("default file source", func(t *testing.T) {
		t.Setenv("SOPS_AGE_KEY", "")
		t.Setenv("SOPS_AGE_KEY_FILE", "")

		cleanup, err := setupDecryptEnv(&AgeIdentity{
			Source:  SourceDefaultFile,
			KeyData: "/default/keys.txt",
		})
		if err != nil {
			t.Fatalf("setupDecryptEnv() error = %v", err)
		}

		if got := os.Getenv("SOPS_AGE_KEY_FILE"); got != "/default/keys.txt" {
			t.Errorf("SOPS_AGE_KEY_FILE = %q, want /default/keys.txt", got)
		}

		cleanup()
	})

	t.Run("unknown source", func(t *testing.T) {
		_, err := setupDecryptEnv(&AgeIdentity{Source: AgeKeySource(99)})
		if err == nil {
			t.Error("setupDecryptEnv() expected error for unknown source")
		}
	})
}
