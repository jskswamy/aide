package secrets

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filippo.io/age"
)

// createEncryptedTestFile creates a sops-encrypted YAML file using the given
// age identity and returns the file path. This is a helper used by rotate tests.
func createEncryptedTestFile(t *testing.T, dir string, name string, identity *age.X25519Identity) string {
	t.Helper()
	filePath := filepath.Join(dir, name+".enc.yaml")

	// Use the existing test fixture as a template - copy it to target dir
	td := testdataDir(t)
	srcFile := filepath.Join(td, "test-secrets.enc.yaml")

	// We need to re-encrypt with the provided identity's public key.
	// Decrypt with the testdata key, then re-encrypt with the new identity.
	keyFile := filepath.Join(td, "age-key.txt")
	ageIdentity := &AgeIdentity{
		Source:  SourceEnvKeyFile,
		KeyData: keyFile,
	}
	secrets, err := DecryptSecretsFile(srcFile, ageIdentity)
	if err != nil {
		t.Fatalf("failed to decrypt test fixture: %v", err)
	}

	// Build plaintext YAML from secrets
	var plaintext strings.Builder
	for k, v := range secrets {
		plaintext.WriteString(k + ": " + v + "\n")
	}

	// Encrypt with the provided identity's recipient
	encrypted, err := encryptWithAge([]byte(plaintext.String()), identity.Recipient().String())
	if err != nil {
		t.Fatalf("failed to encrypt test file: %v", err)
	}

	if err := os.WriteFile(filePath, encrypted, 0600); err != nil {
		t.Fatalf("failed to write encrypted file: %v", err)
	}

	return filePath
}

func TestRotate_AddKey(t *testing.T) {
	// Generate primary key and create encrypted file
	primaryKey, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("failed to generate primary key: %v", err)
	}
	t.Setenv("SOPS_AGE_KEY", primaryKey.String())

	dir := t.TempDir()
	filePath := createEncryptedTestFile(t, dir, "test", primaryKey)

	// Generate a second key to add
	secondKey, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("failed to generate second key: %v", err)
	}

	// Rotate: add the second key
	err = Rotate(filePath, []string{secondKey.Recipient().String()}, nil)
	if err != nil {
		t.Fatalf("Rotate add key failed: %v", err)
	}

	// Verify: list recipients should show both keys
	recipients, err := ListRecipients(filePath)
	if err != nil {
		t.Fatalf("ListRecipients failed: %v", err)
	}
	if len(recipients) != 2 {
		t.Fatalf("expected 2 recipients, got %d: %v", len(recipients), recipients)
	}

	// Verify: both keys can decrypt
	ident := &AgeIdentity{Source: SourceEnvKey, KeyData: primaryKey.String()}
	_, err = DecryptSecretsFile(filePath, ident)
	if err != nil {
		t.Errorf("primary key should still decrypt: %v", err)
	}

	ident2 := &AgeIdentity{Source: SourceEnvKey, KeyData: secondKey.String()}
	_, err = DecryptSecretsFile(filePath, ident2)
	if err != nil {
		t.Errorf("newly added key should decrypt: %v", err)
	}
}

func TestRotate_RemoveKey(t *testing.T) {
	// Generate two keys
	primaryKey, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("failed to generate primary key: %v", err)
	}
	secondKey, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("failed to generate second key: %v", err)
	}
	t.Setenv("SOPS_AGE_KEY", primaryKey.String())

	dir := t.TempDir()
	filePath := createEncryptedTestFile(t, dir, "test", primaryKey)

	// First add the second key
	err = Rotate(filePath, []string{secondKey.Recipient().String()}, nil)
	if err != nil {
		t.Fatalf("Rotate add failed: %v", err)
	}

	// Now remove the second key
	err = Rotate(filePath, nil, []string{secondKey.Recipient().String()})
	if err != nil {
		t.Fatalf("Rotate remove failed: %v", err)
	}

	// Verify only one recipient remains
	recipients, err := ListRecipients(filePath)
	if err != nil {
		t.Fatalf("ListRecipients failed: %v", err)
	}
	if len(recipients) != 1 {
		t.Fatalf("expected 1 recipient, got %d: %v", len(recipients), recipients)
	}
	if recipients[0] != primaryKey.Recipient().String() {
		t.Errorf("remaining recipient should be primary key, got %s", recipients[0])
	}
}

func TestRotate_FileNotFound(t *testing.T) {
	err := Rotate("/nonexistent/file.enc.yaml", []string{"age1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqq2cg5d5"}, nil)
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
	if !strings.Contains(err.Error(), "no such file") && !strings.Contains(err.Error(), "not found") && !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("error should indicate file not found, got: %v", err)
	}
}

func TestRotate_AddDuplicate(t *testing.T) {
	primaryKey, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}
	t.Setenv("SOPS_AGE_KEY", primaryKey.String())

	dir := t.TempDir()
	filePath := createEncryptedTestFile(t, dir, "test", primaryKey)

	// Add the same key that's already a recipient - should be no-op
	err = Rotate(filePath, []string{primaryKey.Recipient().String()}, nil)
	if err != nil {
		t.Fatalf("Rotate with duplicate should not error: %v", err)
	}

	// Verify still only one recipient
	recipients, err := ListRecipients(filePath)
	if err != nil {
		t.Fatalf("ListRecipients failed: %v", err)
	}
	if len(recipients) != 1 {
		t.Errorf("expected 1 recipient after duplicate add, got %d", len(recipients))
	}
}

func TestRotate_RemoveLastKey(t *testing.T) {
	primaryKey, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}
	t.Setenv("SOPS_AGE_KEY", primaryKey.String())

	dir := t.TempDir()
	filePath := createEncryptedTestFile(t, dir, "test", primaryKey)

	// Try to remove the only recipient
	err = Rotate(filePath, nil, []string{primaryKey.Recipient().String()})
	if err == nil {
		t.Fatal("expected error when removing last recipient")
	}
	if !strings.Contains(err.Error(), "cannot remove all recipients") && !strings.Contains(err.Error(), "Cannot remove all recipients") {
		t.Errorf("error should mention cannot remove all recipients, got: %v", err)
	}
}

func TestRotate_InvalidKeyFormat(t *testing.T) {
	primaryKey, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}
	t.Setenv("SOPS_AGE_KEY", primaryKey.String())

	dir := t.TempDir()
	filePath := createEncryptedTestFile(t, dir, "test", primaryKey)

	err = Rotate(filePath, []string{"not-a-valid-key"}, nil)
	if err == nil {
		t.Fatal("expected error for invalid key format")
	}
	if !strings.Contains(err.Error(), "invalid") && !strings.Contains(err.Error(), "Invalid") {
		t.Errorf("error should mention invalid key, got: %v", err)
	}
}

func TestRotate_DataIntegrity(t *testing.T) {
	primaryKey, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}
	t.Setenv("SOPS_AGE_KEY", primaryKey.String())

	dir := t.TempDir()
	filePath := createEncryptedTestFile(t, dir, "test", primaryKey)

	// Decrypt before rotation
	ident := &AgeIdentity{Source: SourceEnvKey, KeyData: primaryKey.String()}
	before, err := DecryptSecretsFile(filePath, ident)
	if err != nil {
		t.Fatalf("failed to decrypt before rotation: %v", err)
	}

	// Add a second key
	secondKey, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("failed to generate second key: %v", err)
	}
	err = Rotate(filePath, []string{secondKey.Recipient().String()}, nil)
	if err != nil {
		t.Fatalf("Rotate failed: %v", err)
	}

	// Decrypt after rotation and compare
	after, err := DecryptSecretsFile(filePath, ident)
	if err != nil {
		t.Fatalf("failed to decrypt after rotation: %v", err)
	}

	// Compare all keys and values
	if len(before) != len(after) {
		t.Fatalf("key count changed: before=%d, after=%d", len(before), len(after))
	}
	for k, v := range before {
		if after[k] != v {
			t.Errorf("value changed for key %q: before=%q, after=%q", k, v, after[k])
		}
	}
}
