package secrets

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/config"
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

// --- List tests ---

// createTestEncryptedFile creates an encrypted file in secretsDir for testing.
func createTestEncryptedFile(t *testing.T, mgr *Manager, secretsDir, name string) {
	t.Helper()
	pubKey := testAgePublicKey(t)
	content := []byte("api_key: sk-test-12345\n")
	if err := mgr.CreateFromContent(name, secretsDir, pubKey, content); err != nil {
		t.Fatalf("failed to create test encrypted file %s: %v", name, err)
	}
}

func TestList_HappyPath(t *testing.T) {
	mgr, tmpDir := testCreateManager(t)
	secretsDir := filepath.Join(tmpDir, "secrets")

	// Create two encrypted files.
	createTestEncryptedFile(t, mgr, secretsDir, "personal")
	createTestEncryptedFile(t, mgr, secretsDir, "work")

	// Create a config that references both files.
	cfg := &config.Config{
		Contexts: map[string]config.Context{
			"personal": {Agent: "claude", SecretsFile: "personal.enc.yaml"},
			"oss":      {Agent: "claude", SecretsFile: "personal.enc.yaml"},
			"work":     {Agent: "claude", SecretsFile: "work.enc.yaml"},
		},
	}

	infos, err := mgr.List(secretsDir, cfg)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if len(infos) != 2 {
		t.Fatalf("expected 2 files, got %d", len(infos))
	}

	// Build a map for easier assertions.
	byName := make(map[string]SecretsFileInfo)
	for _, info := range infos {
		byName[info.Name] = info
	}

	// Check personal file.
	personal, ok := byName["personal.enc.yaml"]
	if !ok {
		t.Fatal("expected personal.enc.yaml in list")
	}
	if personal.Path != filepath.Join(secretsDir, "personal.enc.yaml") {
		t.Errorf("wrong path: %s", personal.Path)
	}
	if len(personal.ReferencedBy) != 2 {
		t.Errorf("expected 2 contexts for personal, got %d: %v", len(personal.ReferencedBy), personal.ReferencedBy)
	}

	// Check work file.
	work, ok := byName["work.enc.yaml"]
	if !ok {
		t.Fatal("expected work.enc.yaml in list")
	}
	if len(work.ReferencedBy) != 1 || work.ReferencedBy[0] != "work" {
		t.Errorf("expected work context for work file, got %v", work.ReferencedBy)
	}
}

func TestList_EmptyDir(t *testing.T) {
	mgr, tmpDir := testCreateManager(t)
	secretsDir := filepath.Join(tmpDir, "secrets")

	infos, err := mgr.List(secretsDir, &config.Config{})
	if err != nil {
		t.Fatalf("List on empty dir failed: %v", err)
	}
	if len(infos) != 0 {
		t.Errorf("expected empty list, got %d items", len(infos))
	}
}

func TestList_NonexistentDir(t *testing.T) {
	mgr, tmpDir := testCreateManager(t)
	secretsDir := filepath.Join(tmpDir, "nonexistent")

	infos, err := mgr.List(secretsDir, &config.Config{})
	if err != nil {
		t.Fatalf("List on nonexistent dir should not error, got: %v", err)
	}
	if len(infos) != 0 {
		t.Errorf("expected empty list, got %d items", len(infos))
	}
}

func TestList_UnreferencedFile(t *testing.T) {
	mgr, tmpDir := testCreateManager(t)
	secretsDir := filepath.Join(tmpDir, "secrets")

	createTestEncryptedFile(t, mgr, secretsDir, "unused")

	// Config with no contexts referencing this file.
	cfg := &config.Config{
		Contexts: map[string]config.Context{
			"personal": {Agent: "claude", SecretsFile: "other.enc.yaml"},
		},
	}

	infos, err := mgr.List(secretsDir, cfg)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(infos) != 1 {
		t.Fatalf("expected 1 file, got %d", len(infos))
	}
	if len(infos[0].ReferencedBy) != 0 {
		t.Errorf("expected no context references, got %v", infos[0].ReferencedBy)
	}
}

func TestList_MultipleContextsSameFile(t *testing.T) {
	mgr, tmpDir := testCreateManager(t)
	secretsDir := filepath.Join(tmpDir, "secrets")

	createTestEncryptedFile(t, mgr, secretsDir, "shared")

	cfg := &config.Config{
		Contexts: map[string]config.Context{
			"ctx1": {Agent: "claude", SecretsFile: "shared.enc.yaml"},
			"ctx2": {Agent: "claude", SecretsFile: "shared.enc.yaml"},
			"ctx3": {Agent: "claude", SecretsFile: "shared.enc.yaml"},
		},
	}

	infos, err := mgr.List(secretsDir, cfg)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(infos) != 1 {
		t.Fatalf("expected 1 file, got %d", len(infos))
	}
	if len(infos[0].ReferencedBy) != 3 {
		t.Errorf("expected 3 context references, got %d: %v", len(infos[0].ReferencedBy), infos[0].ReferencedBy)
	}
}

func TestList_RecipientsExtracted(t *testing.T) {
	mgr, tmpDir := testCreateManager(t)
	secretsDir := filepath.Join(tmpDir, "secrets")
	pubKey := testAgePublicKey(t)

	createTestEncryptedFile(t, mgr, secretsDir, "test-recipients")

	infos, err := mgr.List(secretsDir, &config.Config{})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(infos) != 1 {
		t.Fatalf("expected 1 file, got %d", len(infos))
	}
	if len(infos[0].Recipients) != 1 {
		t.Fatalf("expected 1 recipient, got %d", len(infos[0].Recipients))
	}
	if infos[0].Recipients[0] != pubKey {
		t.Errorf("expected recipient %s, got %s", pubKey, infos[0].Recipients[0])
	}
}

func TestList_CorruptFile(t *testing.T) {
	mgr, tmpDir := testCreateManager(t)
	secretsDir := filepath.Join(tmpDir, "secrets")

	// Create a valid encrypted file too.
	createTestEncryptedFile(t, mgr, secretsDir, "valid")

	// Write a corrupt file with .enc.yaml extension.
	corruptPath := filepath.Join(secretsDir, "corrupt.enc.yaml")
	if err := os.WriteFile(corruptPath, []byte("not valid sops content"), 0o600); err != nil {
		t.Fatal(err)
	}

	infos, err := mgr.List(secretsDir, &config.Config{})
	if err != nil {
		t.Fatalf("List should not fail on corrupt file, got: %v", err)
	}

	if len(infos) != 2 {
		t.Fatalf("expected 2 files (valid + corrupt), got %d", len(infos))
	}

	// Find the corrupt one.
	byName := make(map[string]SecretsFileInfo)
	for _, info := range infos {
		byName[info.Name] = info
	}
	corrupt, ok := byName["corrupt.enc.yaml"]
	if !ok {
		t.Fatal("expected corrupt.enc.yaml in list")
	}
	// Corrupt file should have nil/empty recipients (not crash).
	_ = corrupt
}

func TestList_NilConfig(t *testing.T) {
	mgr, tmpDir := testCreateManager(t)
	secretsDir := filepath.Join(tmpDir, "secrets")

	createTestEncryptedFile(t, mgr, secretsDir, "test")

	// Nil config should not panic.
	infos, err := mgr.List(secretsDir, nil)
	if err != nil {
		t.Fatalf("List with nil config failed: %v", err)
	}
	if len(infos) != 1 {
		t.Fatalf("expected 1 file, got %d", len(infos))
	}
	if len(infos[0].ReferencedBy) != 0 {
		t.Errorf("expected no references with nil config, got %v", infos[0].ReferencedBy)
	}
}

func TestList_WithTestFixture(t *testing.T) {
	mgr, _ := testCreateManager(t)

	// Use the testdata directory which has test-secrets.enc.yaml.
	td := testdataDir(t)

	cfg := &config.Config{
		Contexts: map[string]config.Context{
			"personal": {Agent: "claude", SecretsFile: "test-secrets.enc.yaml"},
		},
	}

	infos, err := mgr.List(td, cfg)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	// Should find at least test-secrets.enc.yaml.
	found := false
	for _, info := range infos {
		if info.Name == "test-secrets.enc.yaml" {
			found = true
			if len(info.Recipients) == 0 {
				t.Error("expected recipients from test-secrets.enc.yaml")
			}
			if len(info.ReferencedBy) != 1 || info.ReferencedBy[0] != "personal" {
				t.Errorf("expected [personal] contexts, got %v", info.ReferencedBy)
			}
		}
	}
	if !found {
		names := make([]string, len(infos))
		for i, info := range infos {
			names[i] = info.Name
		}
		t.Errorf("test-secrets.enc.yaml not found in list: %v", names)
	}
}
