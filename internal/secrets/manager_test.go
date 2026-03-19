package secrets

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filippo.io/age"
)

// testCreateManager sets up an isolated Manager for create tests.
func testCreateManager(t *testing.T) (*Manager, string) {
	t.Helper()
	tmpDir := t.TempDir()
	secretsDir := filepath.Join(tmpDir, "secrets")
	runtimeDir := filepath.Join(tmpDir, "runtime")
	if err := os.MkdirAll(secretsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		t.Fatal(err)
	}

	// Use the testdata age key for encryption/decryption.
	td := testdataDir(t)
	keyFile := filepath.Join(td, "age-key.txt")
	t.Setenv("PATH", "")
	t.Setenv("SOPS_AGE_KEY", "")
	t.Setenv("SOPS_AGE_KEY_FILE", keyFile)
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	mgr := NewManager(secretsDir, runtimeDir)
	return mgr, tmpDir
}

// testAgePublicKey returns the public key from testdata.
func testAgePublicKey(t *testing.T) string {
	t.Helper()
	return "age1zljh2h78fslresdv3fuj2v6sed4k5eulf5m9zmn944yfhgd3y5ssrk6jmg"
}

func TestCreate_ValidContent(t *testing.T) {
	mgr, tmpDir := testCreateManager(t)
	secretsDir := filepath.Join(tmpDir, "secrets")
	pubKey := testAgePublicKey(t)

	content := []byte("api_key: sk-test-12345\ndb_password: supersecret\n")

	err := mgr.CreateFromContent("test", secretsDir, pubKey, content)
	if err != nil {
		t.Fatalf("Create with valid content failed: %v", err)
	}

	// Check encrypted file exists.
	encPath := filepath.Join(secretsDir, "test.enc.yaml")
	if _, err := os.Stat(encPath); os.IsNotExist(err) {
		t.Fatal("encrypted file was not created")
	}

	// Read and verify it contains sops metadata.
	data, err := os.ReadFile(encPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "sops:") {
		t.Error("encrypted file does not contain sops metadata")
	}
	if !strings.Contains(string(data), "ENC[AES256_GCM") {
		t.Error("encrypted file does not contain encrypted values")
	}

	// Verify round-trip: decrypt and check original values.
	td := testdataDir(t)
	keyFile := filepath.Join(td, "age-key.txt")
	identity := &AgeIdentity{Source: SourceEnvKeyFile, KeyData: keyFile}
	secrets, err := DecryptSecretsFile(encPath, identity)
	if err != nil {
		t.Fatalf("failed to decrypt created file: %v", err)
	}
	if secrets["api_key"] != "sk-test-12345" {
		t.Errorf("expected api_key=sk-test-12345, got %q", secrets["api_key"])
	}
	if secrets["db_password"] != "supersecret" {
		t.Errorf("expected db_password=supersecret, got %q", secrets["db_password"])
	}
}

func TestCreate_AlreadyExists(t *testing.T) {
	mgr, tmpDir := testCreateManager(t)
	secretsDir := filepath.Join(tmpDir, "secrets")
	pubKey := testAgePublicKey(t)

	// Pre-create the file.
	encPath := filepath.Join(secretsDir, "existing.enc.yaml")
	if err := os.WriteFile(encPath, []byte("placeholder"), 0o600); err != nil {
		t.Fatal(err)
	}

	content := []byte("key: value\n")
	err := mgr.CreateFromContent("existing", secretsDir, pubKey, content)
	if err == nil {
		t.Fatal("expected error for existing file, got nil")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error should mention 'already exists', got: %v", err)
	}
}

func TestCreate_InvalidName(t *testing.T) {
	mgr, tmpDir := testCreateManager(t)
	secretsDir := filepath.Join(tmpDir, "secrets")
	pubKey := testAgePublicKey(t)
	content := []byte("key: value\n")

	invalidNames := []string{
		"../escape",
		"has.dots",
		"has/slash",
		"has spaces",
		"has@special",
		"",
	}

	for _, name := range invalidNames {
		err := mgr.CreateFromContent(name, secretsDir, pubKey, content)
		if err == nil {
			t.Errorf("expected error for invalid name %q, got nil", name)
		}
	}
}

func TestCreate_ValidNames(t *testing.T) {
	mgr, tmpDir := testCreateManager(t)
	secretsDir := filepath.Join(tmpDir, "secrets")
	pubKey := testAgePublicKey(t)
	content := []byte("key: value\n")

	validNames := []string{
		"simple",
		"with-hyphens",
		"with_underscores",
		"Mixed123",
	}

	for _, name := range validNames {
		err := mgr.CreateFromContent(name, secretsDir, pubKey, content)
		if err != nil {
			t.Errorf("unexpected error for valid name %q: %v", name, err)
		}
	}
}

func TestCreate_FlatYAMLValidation(t *testing.T) {
	mgr, tmpDir := testCreateManager(t)
	secretsDir := filepath.Join(tmpDir, "secrets")
	pubKey := testAgePublicKey(t)

	// Nested YAML should be rejected.
	nested := []byte("parent:\n  child: value\n")
	err := mgr.CreateFromContent("nested-test", secretsDir, pubKey, nested)
	if err == nil {
		t.Fatal("expected error for nested YAML, got nil")
	}
	if !strings.Contains(err.Error(), "flat key-value") {
		t.Errorf("error should mention 'flat key-value', got: %v", err)
	}

	// List YAML should be rejected.
	listYAML := []byte("- item1\n- item2\n")
	err = mgr.CreateFromContent("list-test", secretsDir, pubKey, listYAML)
	if err == nil {
		t.Fatal("expected error for list YAML, got nil")
	}

	// Invalid YAML should be rejected.
	invalidYAML := []byte("[not: valid: flat: yaml")
	err = mgr.CreateFromContent("invalid-test", secretsDir, pubKey, invalidYAML)
	if err == nil {
		t.Fatal("expected error for invalid YAML, got nil")
	}
}

func TestCreate_EmptyContent(t *testing.T) {
	mgr, tmpDir := testCreateManager(t)
	secretsDir := filepath.Join(tmpDir, "secrets")
	pubKey := testAgePublicKey(t)

	// Empty content should be rejected.
	err := mgr.CreateFromContent("empty-test", secretsDir, pubKey, []byte(""))
	if err == nil {
		t.Fatal("expected error for empty content, got nil")
	}
	if !strings.Contains(err.Error(), "No secrets entered") && !strings.Contains(err.Error(), "empty") {
		t.Errorf("error should mention empty/no secrets, got: %v", err)
	}

	// Comment-only content should be rejected.
	commentOnly := []byte("# just a comment\n# another comment\n")
	err = mgr.CreateFromContent("comment-test", secretsDir, pubKey, commentOnly)
	if err == nil {
		t.Fatal("expected error for comment-only content, got nil")
	}
}

func TestCreate_TempFileCleanup(t *testing.T) {
	mgr, tmpDir := testCreateManager(t)
	secretsDir := filepath.Join(tmpDir, "secrets")
	runtimeDir := filepath.Join(tmpDir, "runtime")
	pubKey := testAgePublicKey(t)

	// Even after an error (invalid YAML), runtime dir should be clean.
	nested := []byte("parent:\n  child: value\n")
	_ = mgr.CreateFromContent("cleanup-test", secretsDir, pubKey, nested)

	// Check no temp files remain in runtime dir.
	entries, err := os.ReadDir(runtimeDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "aide-secrets-") {
			t.Errorf("temp directory not cleaned up: %s", e.Name())
		}
	}
}

func TestCreate_NumberValues(t *testing.T) {
	mgr, tmpDir := testCreateManager(t)
	secretsDir := filepath.Join(tmpDir, "secrets")
	pubKey := testAgePublicKey(t)

	// Numbers should be accepted (they're scalar values).
	content := []byte("port: 8080\napi_key: sk-test\n")
	err := mgr.CreateFromContent("number-test", secretsDir, pubKey, content)
	if err != nil {
		t.Fatalf("unexpected error for number values: %v", err)
	}

	// Verify round-trip.
	encPath := filepath.Join(secretsDir, "number-test.enc.yaml")
	td := testdataDir(t)
	keyFile := filepath.Join(td, "age-key.txt")
	identity := &AgeIdentity{Source: SourceEnvKeyFile, KeyData: keyFile}
	secrets, err := DecryptSecretsFile(encPath, identity)
	if err != nil {
		t.Fatalf("failed to decrypt: %v", err)
	}
	if secrets["port"] != "8080" {
		t.Errorf("expected port=8080, got %q", secrets["port"])
	}
}

func TestCreate_SecretsDirCreated(t *testing.T) {
	mgr, tmpDir := testCreateManager(t)
	pubKey := testAgePublicKey(t)

	// Use a secretsDir that doesn't exist yet.
	newSecretsDir := filepath.Join(tmpDir, "new-secrets-dir")
	content := []byte("key: value\n")

	err := mgr.CreateFromContent("auto-dir-test", newSecretsDir, pubKey, content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The dir and file should have been created.
	encPath := filepath.Join(newSecretsDir, "auto-dir-test.enc.yaml")
	if _, err := os.Stat(encPath); os.IsNotExist(err) {
		t.Fatal("encrypted file was not created in new directory")
	}
}

// --- Edit tests ---

func TestEdit_ValidContent(t *testing.T) {
	mgr, tmpDir := testCreateManager(t)
	secretsDir := filepath.Join(tmpDir, "secrets")
	pubKey := testAgePublicKey(t)

	// Create an encrypted file first.
	originalContent := []byte("api_key: sk-test-12345\ndb_password: supersecret\n")
	err := mgr.CreateFromContent("edit-test", secretsDir, pubKey, originalContent)
	if err != nil {
		t.Fatalf("failed to create initial file: %v", err)
	}

	// Edit with new content that adds a key.
	newContent := []byte("api_key: sk-test-12345\ndb_password: supersecret\nnew_key: new-value\n")
	err = mgr.EditFromContent("edit-test", secretsDir, newContent)
	if err != nil {
		t.Fatalf("EditFromContent failed: %v", err)
	}

	// Decrypt and verify the new content.
	encPath := filepath.Join(secretsDir, "edit-test.enc.yaml")
	td := testdataDir(t)
	keyFile := filepath.Join(td, "age-key.txt")
	identity := &AgeIdentity{Source: SourceEnvKeyFile, KeyData: keyFile}
	secrets, err := DecryptSecretsFile(encPath, identity)
	if err != nil {
		t.Fatalf("failed to decrypt edited file: %v", err)
	}
	if secrets["api_key"] != "sk-test-12345" {
		t.Errorf("expected api_key=sk-test-12345, got %q", secrets["api_key"])
	}
	if secrets["new_key"] != "new-value" {
		t.Errorf("expected new_key=new-value, got %q", secrets["new_key"])
	}
}

func TestEdit_FileNotFound(t *testing.T) {
	mgr, tmpDir := testCreateManager(t)
	secretsDir := filepath.Join(tmpDir, "secrets")

	err := mgr.EditFromContent("nonexistent", secretsDir, []byte("key: value\n"))
	if err == nil {
		t.Fatal("expected error for nonexistent file, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention 'not found', got: %v", err)
	}
}

func TestEdit_PreservesRecipients(t *testing.T) {
	// Generate a fresh key and create encrypted file with it.
	primaryKey, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("failed to generate primary key: %v", err)
	}
	t.Setenv("SOPS_AGE_KEY", primaryKey.String())
	t.Setenv("SOPS_AGE_KEY_FILE", "")
	t.Setenv("PATH", "")

	tmpDir := t.TempDir()
	secretsDir := filepath.Join(tmpDir, "secrets")
	runtimeDir := filepath.Join(tmpDir, "runtime")
	os.MkdirAll(secretsDir, 0o700)
	os.MkdirAll(runtimeDir, 0o700)
	mgr := NewManager(secretsDir, runtimeDir)

	// Create the file with the primary key.
	content := []byte("api_key: original\n")
	encrypted, err := encryptWithAge(content, primaryKey.Recipient().String())
	if err != nil {
		t.Fatalf("failed to encrypt: %v", err)
	}
	encPath := filepath.Join(secretsDir, "preserve-test.enc.yaml")
	if err := os.WriteFile(encPath, encrypted, 0o600); err != nil {
		t.Fatal(err)
	}

	// Get recipients before edit.
	recipientsBefore, err := ListRecipients(encPath)
	if err != nil {
		t.Fatalf("failed to list recipients before: %v", err)
	}

	// Edit the file.
	newContent := []byte("api_key: updated\n")
	err = mgr.EditFromContent("preserve-test", secretsDir, newContent)
	if err != nil {
		t.Fatalf("EditFromContent failed: %v", err)
	}

	// Get recipients after edit.
	recipientsAfter, err := ListRecipients(encPath)
	if err != nil {
		t.Fatalf("failed to list recipients after: %v", err)
	}

	// Recipients should be identical.
	if len(recipientsBefore) != len(recipientsAfter) {
		t.Fatalf("recipient count changed: before=%d, after=%d", len(recipientsBefore), len(recipientsAfter))
	}
	for i, r := range recipientsBefore {
		if recipientsAfter[i] != r {
			t.Errorf("recipient %d changed: before=%s, after=%s", i, r, recipientsAfter[i])
		}
	}

	// Verify the primary key can still decrypt.
	ident := &AgeIdentity{Source: SourceEnvKey, KeyData: primaryKey.String()}
	secrets, err := DecryptSecretsFile(encPath, ident)
	if err != nil {
		t.Fatalf("primary key should still decrypt: %v", err)
	}
	if secrets["api_key"] != "updated" {
		t.Errorf("expected api_key=updated, got %q", secrets["api_key"])
	}
}

func TestEdit_TempFileCleanup(t *testing.T) {
	mgr, tmpDir := testCreateManager(t)
	secretsDir := filepath.Join(tmpDir, "secrets")
	runtimeDir := filepath.Join(tmpDir, "runtime")
	pubKey := testAgePublicKey(t)

	// Create an encrypted file first.
	content := []byte("api_key: test\n")
	err := mgr.CreateFromContent("cleanup-edit", secretsDir, pubKey, content)
	if err != nil {
		t.Fatalf("failed to create initial file: %v", err)
	}

	// Edit with invalid YAML to trigger an error; temp should still be cleaned.
	invalidContent := []byte("parent:\n  child: nested\n")
	_ = mgr.EditFromContent("cleanup-edit", secretsDir, invalidContent)

	// Check no temp files remain in runtime dir.
	entries, err := os.ReadDir(runtimeDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "aide-secrets-") {
			t.Errorf("temp directory not cleaned up: %s", e.Name())
		}
	}
}

func TestEdit_InvalidYAMLRejected(t *testing.T) {
	mgr, tmpDir := testCreateManager(t)
	secretsDir := filepath.Join(tmpDir, "secrets")
	pubKey := testAgePublicKey(t)

	// Create an encrypted file first.
	content := []byte("api_key: test\n")
	err := mgr.CreateFromContent("invalid-edit", secretsDir, pubKey, content)
	if err != nil {
		t.Fatalf("failed to create initial file: %v", err)
	}

	// Read original file bytes for comparison.
	encPath := filepath.Join(secretsDir, "invalid-edit.enc.yaml")
	originalBytes, err := os.ReadFile(encPath)
	if err != nil {
		t.Fatal(err)
	}

	// Edit with nested YAML (non-flat) should be rejected.
	nestedContent := []byte("parent:\n  child: nested\n")
	err = mgr.EditFromContent("invalid-edit", secretsDir, nestedContent)
	if err == nil {
		t.Fatal("expected error for non-flat YAML, got nil")
	}

	// Original file should be unchanged.
	afterBytes, err := os.ReadFile(encPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(originalBytes) != string(afterBytes) {
		t.Error("original file was modified despite invalid edit content")
	}
}
