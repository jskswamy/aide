package secrets

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filippo.io/age"
	"go.uber.org/mock/gomock"

	smocks "github.com/jskswamy/aide/internal/secrets/mocks"
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
	if !strings.Contains(err.Error(), "no secrets entered") && !strings.Contains(err.Error(), "empty") {
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
	if err := os.MkdirAll(secretsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		t.Fatal(err)
	}
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

func TestValidateName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"valid simple", "personal", false},
		{"valid with hyphen", "my-secrets", false},
		{"valid with underscore", "my_secrets", false},
		{"valid with numbers", "secret123", false},
		{"empty", "", true},
		{"starts with hyphen", "-invalid", true},
		{"starts with underscore", "_invalid", true},
		{"has spaces", "my secrets", true},
		{"has dots", "my.secrets", true},
		{"has slash", "path/name", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateName(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateName(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}

func TestValidateContent(t *testing.T) {
	tests := []struct {
		name    string
		content []byte
		wantErr bool
	}{
		{"valid", []byte("key: value\n"), false},
		{"empty", []byte(""), true},
		{"whitespace only", []byte("   \n  \n"), true},
		{"comments only", []byte("# comment\n# another\n"), true},
		{"comments with value", []byte("# comment\nkey: value\n"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateContent(tt.content)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateContent() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateFlatYAML(t *testing.T) {
	tests := []struct {
		name    string
		content []byte
		wantErr bool
	}{
		{"flat map", []byte("key: value\nnum: 42\n"), false},
		{"boolean value", []byte("flag: true\n"), false},
		{"nested map", []byte("parent:\n  child: value\n"), true},
		{"list value", []byte("items:\n  - one\n  - two\n"), true},
		{"invalid yaml", []byte("not: valid: yaml: {{{\n"), true},
		{"scalar only", []byte("just a string\n"), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateFlatYAML(tt.content)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateFlatYAML() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestResolveEditor(t *testing.T) {
	t.Run("EDITOR set", func(t *testing.T) {
		t.Setenv("EDITOR", "/usr/bin/nano")
		t.Setenv("VISUAL", "")
		got := resolveEditor()
		if got != "/usr/bin/nano" {
			t.Errorf("resolveEditor() = %q, want /usr/bin/nano", got)
		}
	})

	t.Run("VISUAL set no EDITOR", func(t *testing.T) {
		t.Setenv("EDITOR", "")
		t.Setenv("VISUAL", "/usr/bin/code")
		got := resolveEditor()
		if got != "/usr/bin/code" {
			t.Errorf("resolveEditor() = %q, want /usr/bin/code", got)
		}
	})

	t.Run("EDITOR takes precedence", func(t *testing.T) {
		t.Setenv("EDITOR", "/usr/bin/nano")
		t.Setenv("VISUAL", "/usr/bin/code")
		got := resolveEditor()
		if got != "/usr/bin/nano" {
			t.Errorf("resolveEditor() = %q, want /usr/bin/nano", got)
		}
	})

	t.Run("fallback to vi", func(t *testing.T) {
		t.Setenv("EDITOR", "")
		t.Setenv("VISUAL", "")
		got := resolveEditor()
		if got != "vi" {
			t.Errorf("resolveEditor() = %q, want vi", got)
		}
	})
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

func TestCreate_WithMockEditor(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockEditor := smocks.NewMockEditorRunner(ctrl)

	tmpDir := t.TempDir()
	secretsDir := filepath.Join(tmpDir, "secrets")
	runtimeDir := filepath.Join(tmpDir, "runtime")
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		t.Fatal(err)
	}

	// Generate a real age key pair for encryption.
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	pubKey := identity.Recipient().String()

	// Mock editor writes valid YAML to the temp file.
	mockEditor.EXPECT().
		Run(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ string, args []string, _ io.Reader, _, _ io.Writer) error {
			return os.WriteFile(args[0], []byte("api_key: sk-test-123\n"), 0o600)
		})

	t.Setenv("EDITOR", "fake-editor")
	mgr := NewManagerWithEditor(secretsDir, runtimeDir, mockEditor)
	err = mgr.Create("test", secretsDir, pubKey)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// Verify encrypted file was created.
	encPath := filepath.Join(secretsDir, "test.enc.yaml")
	if _, err := os.Stat(encPath); os.IsNotExist(err) {
		t.Error("encrypted file not created")
	}
}

func TestCreate_EditorError(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockEditor := smocks.NewMockEditorRunner(ctrl)

	tmpDir := t.TempDir()
	secretsDir := filepath.Join(tmpDir, "secrets")
	runtimeDir := filepath.Join(tmpDir, "runtime")
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		t.Fatal(err)
	}

	identity, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}

	mockEditor.EXPECT().
		Run(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(fmt.Errorf("editor crashed"))

	t.Setenv("EDITOR", "fake-editor")
	mgr := NewManagerWithEditor(secretsDir, runtimeDir, mockEditor)
	err = mgr.Create("test", secretsDir, identity.Recipient().String())
	if err == nil {
		t.Fatal("Create() expected error when editor fails")
	}
	if !strings.Contains(err.Error(), "editor exited with error") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestCreate_EditorEmptyContent(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockEditor := smocks.NewMockEditorRunner(ctrl)

	tmpDir := t.TempDir()
	secretsDir := filepath.Join(tmpDir, "secrets")
	runtimeDir := filepath.Join(tmpDir, "runtime")
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		t.Fatal(err)
	}

	identity, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}

	mockEditor.EXPECT().
		Run(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ string, args []string, _ io.Reader, _, _ io.Writer) error {
			return os.WriteFile(args[0], []byte("# just comments\n"), 0o600)
		})

	t.Setenv("EDITOR", "fake-editor")
	mgr := NewManagerWithEditor(secretsDir, runtimeDir, mockEditor)
	err = mgr.Create("test", secretsDir, identity.Recipient().String())
	if err == nil {
		t.Fatal("Create() expected error for empty content")
	}
	if !strings.Contains(err.Error(), "no secrets entered") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestEdit_WithMockEditor(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockEditor := smocks.NewMockEditorRunner(ctrl)

	tmpDir := t.TempDir()
	secretsDir := filepath.Join(tmpDir, "secrets")
	runtimeDir := filepath.Join(tmpDir, "runtime")
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(secretsDir, 0o700); err != nil {
		t.Fatal(err)
	}

	// Generate age key
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	pubKey := identity.Recipient().String()

	// Create an initial encrypted file
	mgr := NewManagerWithEditor(secretsDir, runtimeDir, mockEditor)
	err = mgr.CreateFromContent("test", secretsDir, pubKey, []byte("old_key: old_value\n"))
	if err != nil {
		t.Fatalf("CreateFromContent() setup error = %v", err)
	}

	// Set up env for decryption
	t.Setenv("SOPS_AGE_KEY", identity.String())
	t.Setenv("SOPS_AGE_KEY_FILE", "")
	t.Setenv("EDITOR", "fake-editor")

	// Mock editor modifies the temp file with new content
	mockEditor.EXPECT().
		Run(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ string, args []string, _ io.Reader, _, _ io.Writer) error {
			return os.WriteFile(args[0], []byte("new_key: new_value\n"), 0o600)
		})

	err = mgr.Edit("test", secretsDir)
	if err != nil {
		t.Fatalf("Edit() error = %v", err)
	}

	// Verify the file was re-encrypted by decrypting it
	secrets, err := DecryptSecretsFile(
		filepath.Join(secretsDir, "test.enc.yaml"),
		&AgeIdentity{Source: SourceEnvKey, KeyData: identity.String()},
	)
	if err != nil {
		t.Fatalf("DecryptSecretsFile() error = %v", err)
	}
	if secrets["new_key"] != "new_value" {
		t.Errorf("expected new_key=new_value, got %v", secrets)
	}
}

func TestCreate_InvalidNameViaCreate(t *testing.T) {
	// Test that Create (not CreateFromContent) rejects invalid names early.
	tmpDir := t.TempDir()
	runtimeDir := filepath.Join(tmpDir, "runtime")
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	mgr := NewManager("", runtimeDir)

	err := mgr.Create("", filepath.Join(tmpDir, "secrets"), "age1fake")
	if err == nil {
		t.Fatal("expected error for empty name in Create, got nil")
	}
	if !strings.Contains(err.Error(), "invalid secrets name") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCreate_AlreadyExistsViaCreate(t *testing.T) {
	// Test that Create (not CreateFromContent) rejects already-existing file.
	tmpDir := t.TempDir()
	secretsDir := filepath.Join(tmpDir, "secrets")
	runtimeDir := filepath.Join(tmpDir, "runtime")
	if err := os.MkdirAll(secretsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		t.Fatal(err)
	}

	// Pre-create the encrypted file.
	encPath := filepath.Join(secretsDir, "existing.enc.yaml")
	if err := os.WriteFile(encPath, []byte("placeholder"), 0o600); err != nil {
		t.Fatal(err)
	}

	mgr := NewManager(secretsDir, runtimeDir)
	err := mgr.Create("existing", secretsDir, "age1fake")
	if err == nil {
		t.Fatal("expected error for existing file in Create, got nil")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestEdit_InvalidNameViaEdit(t *testing.T) {
	// Test that Edit rejects invalid names early.
	tmpDir := t.TempDir()
	mgr := NewManager("", tmpDir)

	err := mgr.Edit("../bad-name", filepath.Join(tmpDir, "secrets"))
	if err == nil {
		t.Fatal("expected error for invalid name in Edit, got nil")
	}
	if !strings.Contains(err.Error(), "invalid secrets name") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestEdit_FileNotFoundViaEdit(t *testing.T) {
	// Test that Edit returns error for non-existent file.
	tmpDir := t.TempDir()
	secretsDir := filepath.Join(tmpDir, "secrets")
	if err := os.MkdirAll(secretsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	mgr := NewManager(secretsDir, tmpDir)

	err := mgr.Edit("nonexistent", secretsDir)
	if err == nil {
		t.Fatal("expected error for nonexistent file in Edit, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSecureTempDir_InvalidBaseDir(t *testing.T) {
	mgr := NewManager("", "/nonexistent/path/for/test")
	_, _, err := mgr.secureTempDir("test-")
	if err == nil {
		t.Fatal("expected error for non-existent base dir, got nil")
	}
}

func TestEditFromContent_InvalidName(t *testing.T) {
	tmpDir := t.TempDir()
	mgr := NewManager("", tmpDir)

	err := mgr.EditFromContent("", filepath.Join(tmpDir, "secrets"), []byte("key: val\n"))
	if err == nil {
		t.Fatal("expected error for empty name in EditFromContent, got nil")
	}
}

func TestSecureTempDir(t *testing.T) {
	t.Run("with runtime dir", func(t *testing.T) {
		runtimeDir := t.TempDir()
		mgr := NewManager("", runtimeDir)
		dir, cleanup, err := mgr.secureTempDir("test-")
		if err != nil {
			t.Fatalf("secureTempDir() error = %v", err)
		}
		defer cleanup()

		if !strings.HasPrefix(dir, runtimeDir) {
			t.Errorf("temp dir %q not under runtimeDir %q", dir, runtimeDir)
		}

		info, err := os.Stat(dir)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o700 {
			t.Errorf("temp dir permissions = %o, want 700", info.Mode().Perm())
		}
	})

	t.Run("cleanup removes dir", func(t *testing.T) {
		runtimeDir := t.TempDir()
		mgr := NewManager("", runtimeDir)
		dir, cleanup, err := mgr.secureTempDir("test-")
		if err != nil {
			t.Fatal(err)
		}
		cleanup()

		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Error("cleanup did not remove temp dir")
		}
	})

	t.Run("fallback to os temp", func(t *testing.T) {
		mgr := NewManager("", "")
		dir, cleanup, err := mgr.secureTempDir("test-")
		if err != nil {
			t.Fatal(err)
		}
		defer cleanup()

		if dir == "" {
			t.Error("expected non-empty dir")
		}
	})
}

func TestEditFromContent_FileNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	mgr := NewManager("", tmpDir)

	err := mgr.EditFromContent("nonexistent", filepath.Join(tmpDir, "secrets"), []byte("key: val\n"))
	if err == nil {
		t.Fatal("expected error for missing encrypted file")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got: %v", err)
	}
}

func TestEditFromContent_EmptyContent(t *testing.T) {
	tmpDir := t.TempDir()
	secretsDir := filepath.Join(tmpDir, "secrets")
	if err := os.MkdirAll(secretsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	// Create a dummy encrypted file so the stat check passes
	if err := os.WriteFile(filepath.Join(secretsDir, "test.enc.yaml"), []byte("dummy"), 0o600); err != nil {
		t.Fatal(err)
	}

	mgr := NewManager("", tmpDir)
	err := mgr.EditFromContent("test", secretsDir, []byte(""))
	if err == nil {
		t.Fatal("expected error for empty content")
	}
}

func TestEditFromContent_NestedYAML(t *testing.T) {
	tmpDir := t.TempDir()
	secretsDir := filepath.Join(tmpDir, "secrets")
	if err := os.MkdirAll(secretsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(secretsDir, "test.enc.yaml"), []byte("dummy"), 0o600); err != nil {
		t.Fatal(err)
	}

	mgr := NewManager("", tmpDir)
	err := mgr.EditFromContent("test", secretsDir, []byte("key:\n  nested: val\n"))
	if err == nil {
		t.Fatal("expected error for nested YAML")
	}
}
