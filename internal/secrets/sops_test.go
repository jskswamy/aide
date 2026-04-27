package secrets

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"filippo.io/age"
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

func TestDecryptSecretsFile_WrongKey_SurfacesDetailedError(t *testing.T) {
	td := testdataDir(t)
	encFile := filepath.Join(td, "test-secrets.enc.yaml")

	// Use a wrong key so sops returns a getDataKeyError with UserError details.
	identity := &AgeIdentity{
		Source:  SourceEnvKey,
		KeyData: "AGE-SECRET-KEY-1QQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQ",
	}

	_, err := DecryptSecretsFile(encFile, identity)
	if err == nil {
		t.Fatal("expected error when decrypting with wrong key, got nil")
	}

	errMsg := err.Error()

	// The error should contain the detailed sops UserError output showing
	// per-key failure reasons, not just "0 successful groups required, got 0".
	if strings.Contains(errMsg, "0 successful groups required") {
		t.Errorf("error should surface detailed sops error, not the summary. Got: %s", errMsg)
	}

	// Should contain actual failure detail from the age key decryption.
	if !strings.Contains(errMsg, "FAILED") && !strings.Contains(errMsg, "failed") {
		t.Errorf("error should contain failure details from sops, got: %s", errMsg)
	}

	// Should NOT contain the hardcoded "is your YubiKey plugged in?" message
	// when the identity source is not a YubiKey.
	if strings.Contains(errMsg, "YubiKey plugged in") {
		t.Errorf("non-YubiKey decryption error should not mention YubiKey, got: %s", errMsg)
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

func TestDecryptSecretsFile_ScalarTypes(t *testing.T) {
	// Create an encrypted file containing nil, int, float, and bool values,
	// then decrypt it and verify the type conversion branches.
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("failed to generate age key: %v", err)
	}
	pubKey := identity.Recipient().String()
	t.Setenv("SOPS_AGE_KEY", identity.String())
	t.Setenv("SOPS_AGE_KEY_FILE", "")

	// YAML with various scalar types: int, bool, float, and nil (~).
	content := []byte("int_val: 42\nbool_val: true\nfloat_val: 3.14\nnil_val: ~\nstr_val: hello\n")
	encrypted, err := encryptWithAge(content, pubKey)
	if err != nil {
		t.Fatalf("encryptWithAge failed: %v", err)
	}

	tmpDir := t.TempDir()
	encFile := filepath.Join(tmpDir, "scalars.enc.yaml")
	if err := os.WriteFile(encFile, encrypted, 0o600); err != nil {
		t.Fatal(err)
	}

	ident := &AgeIdentity{Source: SourceEnvKey, KeyData: identity.String()}
	secrets, err := DecryptSecretsFile(encFile, ident)
	if err != nil {
		t.Fatalf("DecryptSecretsFile failed: %v", err)
	}

	// Verify type conversions.
	if secrets["int_val"] != "42" {
		t.Errorf("int_val = %q, want %q", secrets["int_val"], "42")
	}
	if secrets["bool_val"] != "true" {
		t.Errorf("bool_val = %q, want %q", secrets["bool_val"], "true")
	}
	if secrets["float_val"] != "3.14" {
		t.Errorf("float_val = %q, want %q", secrets["float_val"], "3.14")
	}
	if secrets["nil_val"] != "" {
		t.Errorf("nil_val = %q, want empty string", secrets["nil_val"])
	}
	if secrets["str_val"] != "hello" {
		t.Errorf("str_val = %q, want %q", secrets["str_val"], "hello")
	}
}

func TestDecryptSecretsFile_NestedMapRejected(t *testing.T) {
	// Create an encrypted file containing a nested map value,
	// which should be rejected by the type conversion logic.
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("failed to generate age key: %v", err)
	}
	pubKey := identity.Recipient().String()
	t.Setenv("SOPS_AGE_KEY", identity.String())
	t.Setenv("SOPS_AGE_KEY_FILE", "")

	content := []byte("flat_key: value\nnested:\n  child: deep\n")
	encrypted, err := encryptWithAge(content, pubKey)
	if err != nil {
		t.Fatalf("encryptWithAge failed: %v", err)
	}

	tmpDir := t.TempDir()
	encFile := filepath.Join(tmpDir, "nested.enc.yaml")
	if err := os.WriteFile(encFile, encrypted, 0o600); err != nil {
		t.Fatal(err)
	}

	ident := &AgeIdentity{Source: SourceEnvKey, KeyData: identity.String()}
	_, err = DecryptSecretsFile(encFile, ident)
	if err == nil {
		t.Fatal("expected error for nested map value, got nil")
	}
	if !strings.Contains(err.Error(), "non-string value") {
		t.Errorf("error should mention non-string value, got: %v", err)
	}
}

func TestSetupDecryptEnv_RestoresEnv(t *testing.T) {
	// Verify that cleanup actually restores environment variables
	// to their original state, including unsetting vars that did not exist.
	t.Run("restores previously set vars", func(t *testing.T) {
		t.Setenv("SOPS_AGE_KEY", "original-key")
		t.Setenv("SOPS_AGE_KEY_FILE", "original-file")

		cleanup, err := setupDecryptEnv(&AgeIdentity{
			Source:  SourceEnvKey,
			KeyData: "AGE-SECRET-KEY-1NEWKEY",
		})
		if err != nil {
			t.Fatal(err)
		}

		// Env should be changed.
		if got := os.Getenv("SOPS_AGE_KEY"); got != "AGE-SECRET-KEY-1NEWKEY" {
			t.Errorf("before cleanup: SOPS_AGE_KEY = %q, want new value", got)
		}

		cleanup()

		// After cleanup, should be restored.
		if got := os.Getenv("SOPS_AGE_KEY"); got != "original-key" {
			t.Errorf("after cleanup: SOPS_AGE_KEY = %q, want original-key", got)
		}
		if got := os.Getenv("SOPS_AGE_KEY_FILE"); got != "original-file" {
			t.Errorf("after cleanup: SOPS_AGE_KEY_FILE = %q, want original-file", got)
		}
	})

	t.Run("restores key file source vars", func(t *testing.T) {
		t.Setenv("SOPS_AGE_KEY", "orig-key")
		t.Setenv("SOPS_AGE_KEY_FILE", "orig-file")

		cleanup, err := setupDecryptEnv(&AgeIdentity{
			Source:  SourceEnvKeyFile,
			KeyData: "/new/path/key.txt",
		})
		if err != nil {
			t.Fatal(err)
		}

		if got := os.Getenv("SOPS_AGE_KEY_FILE"); got != "/new/path/key.txt" {
			t.Errorf("before cleanup: SOPS_AGE_KEY_FILE = %q", got)
		}
		if got := os.Getenv("SOPS_AGE_KEY"); got != "" {
			t.Errorf("before cleanup: SOPS_AGE_KEY should be empty, got %q", got)
		}

		cleanup()

		if got := os.Getenv("SOPS_AGE_KEY"); got != "orig-key" {
			t.Errorf("after cleanup: SOPS_AGE_KEY = %q, want orig-key", got)
		}
		if got := os.Getenv("SOPS_AGE_KEY_FILE"); got != "orig-file" {
			t.Errorf("after cleanup: SOPS_AGE_KEY_FILE = %q, want orig-file", got)
		}
	})

	t.Run("restores default file source vars", func(t *testing.T) {
		t.Setenv("SOPS_AGE_KEY", "orig-key-2")
		t.Setenv("SOPS_AGE_KEY_FILE", "orig-file-2")

		cleanup, err := setupDecryptEnv(&AgeIdentity{
			Source:  SourceDefaultFile,
			KeyData: "/default/keys.txt",
		})
		if err != nil {
			t.Fatal(err)
		}

		cleanup()

		if got := os.Getenv("SOPS_AGE_KEY"); got != "orig-key-2" {
			t.Errorf("after cleanup: SOPS_AGE_KEY = %q, want orig-key-2", got)
		}
		if got := os.Getenv("SOPS_AGE_KEY_FILE"); got != "orig-file-2" {
			t.Errorf("after cleanup: SOPS_AGE_KEY_FILE = %q, want orig-file-2", got)
		}
	})
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
