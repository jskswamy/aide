package guards_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt"
	"github.com/jskswamy/aide/pkg/seatbelt/guards"
)

func TestSSHKeysGuard_Metadata(t *testing.T) {
	g := guards.SSHKeysGuard()

	if g.Name() != "ssh-keys" {
		t.Errorf("expected Name() = %q, got %q", "ssh-keys", g.Name())
	}
	if g.Type() != "default" {
		t.Errorf("expected Type() = %q, got %q", "default", g.Type())
	}
	if g.Description() == "" {
		t.Error("expected non-empty Description()")
	}
}

func TestSSHKeysGuard_DirectoryNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := &seatbelt.Context{HomeDir: tmpDir}
	// No .ssh directory exists under tmpDir

	g := guards.SSHKeysGuard()
	result := g.Rules(ctx)

	if len(result.Rules) != 0 {
		t.Errorf("expected 0 rules, got %d", len(result.Rules))
	}
	if len(result.Skipped) != 1 {
		t.Fatalf("expected 1 skip message, got %d", len(result.Skipped))
	}
	if !strings.Contains(result.Skipped[0], ".ssh") {
		t.Errorf("skip message should mention .ssh, got: %s", result.Skipped[0])
	}
}

func TestSSHKeysGuard_EmptyDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	sshDir := filepath.Join(tmpDir, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}

	ctx := &seatbelt.Context{HomeDir: tmpDir}
	g := guards.SSHKeysGuard()
	result := g.Rules(ctx)

	if len(result.Rules) != 0 {
		t.Errorf("expected 0 rules for empty dir, got %d", len(result.Rules))
	}
	if len(result.Protected) != 0 {
		t.Errorf("expected 0 protected, got %d", len(result.Protected))
	}
	if len(result.Allowed) != 0 {
		t.Errorf("expected 0 allowed, got %d", len(result.Allowed))
	}
}

func TestSSHKeysGuard_OnlyPublicKeys(t *testing.T) {
	tmpDir := t.TempDir()
	sshDir := filepath.Join(tmpDir, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}

	pubFiles := []string{"id_rsa.pub", "id_ed25519.pub", "deploy.pub"}
	for _, name := range pubFiles {
		if err := os.WriteFile(filepath.Join(sshDir, name), []byte("pubkey"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	ctx := &seatbelt.Context{HomeDir: tmpDir}
	g := guards.SSHKeysGuard()
	result := g.Rules(ctx)

	if len(result.Protected) != 0 {
		t.Errorf("expected 0 protected, got %d: %v", len(result.Protected), result.Protected)
	}
	if len(result.Allowed) != len(pubFiles) {
		t.Errorf("expected %d allowed, got %d: %v", len(pubFiles), len(result.Allowed), result.Allowed)
	}

	output := renderTestRules(result.Rules)
	for _, name := range pubFiles {
		fullPath := filepath.Join(sshDir, name)
		if !strings.Contains(output, fullPath) {
			t.Errorf("expected allow rule for %s", fullPath)
		}
	}
}

func TestSSHKeysGuard_MixedFiles(t *testing.T) {
	tmpDir := t.TempDir()
	sshDir := filepath.Join(tmpDir, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}

	// Safe files
	safeFiles := []string{"known_hosts", "known_hosts.old", "config", "authorized_keys", "id_rsa.pub", "environment"}
	for _, name := range safeFiles {
		if err := os.WriteFile(filepath.Join(sshDir, name), []byte("safe"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Private key files (should be denied)
	privateFiles := []string{"id_rsa", "id_ed25519", "my-deploy-key"}
	for _, name := range privateFiles {
		if err := os.WriteFile(filepath.Join(sshDir, name), []byte("private"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	// Subdirectory (should be skipped entirely)
	if err := os.MkdirAll(filepath.Join(sshDir, "sockets"), 0o700); err != nil {
		t.Fatal(err)
	}

	ctx := &seatbelt.Context{HomeDir: tmpDir}
	g := guards.SSHKeysGuard()
	result := g.Rules(ctx)

	// Verify counts
	if len(result.Protected) != 3 {
		t.Errorf("expected 3 protected, got %d: %v", len(result.Protected), result.Protected)
	}
	if len(result.Allowed) != 6 {
		t.Errorf("expected 6 allowed, got %d: %v", len(result.Allowed), result.Allowed)
	}

	output := renderTestRules(result.Rules)

	// Verify deny rules exist for each protected path
	for _, name := range privateFiles {
		fullPath := filepath.Join(sshDir, name)
		if !strings.Contains(output, fmt.Sprintf(`(deny file-read-data (literal "%s"))`, fullPath)) {
			t.Errorf("expected deny file-read-data rule for %s", fullPath)
		}
		if !strings.Contains(output, fmt.Sprintf(`(deny file-write* (literal "%s"))`, fullPath)) {
			t.Errorf("expected deny file-write* rule for %s", fullPath)
		}
	}

	// Verify allow rules exist for each allowed path
	for _, name := range safeFiles {
		fullPath := filepath.Join(sshDir, name)
		if !strings.Contains(output, fmt.Sprintf(`(allow file-read* (literal "%s"))`, fullPath)) {
			t.Errorf("expected allow file-read* rule for %s", fullPath)
		}
	}

	// Verify metadata allow for .ssh directory itself
	if !strings.Contains(output, fmt.Sprintf(`(allow file-read-metadata (literal "%s"))`, sshDir)) {
		t.Error("expected metadata allow rule for .ssh directory")
	}
}

func TestSSHKeysGuard_SymlinkToFile(t *testing.T) {
	tmpDir := t.TempDir()
	sshDir := filepath.Join(tmpDir, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}

	// Create a real private key file outside .ssh
	realKey := filepath.Join(tmpDir, "actual_key")
	if err := os.WriteFile(realKey, []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Symlink that doesn't match allowlist -> should be denied
	if err := os.Symlink(realKey, filepath.Join(sshDir, "my-key-link")); err != nil {
		t.Fatal(err)
	}

	// Symlink that matches allowlist (.pub suffix) -> should be allowed
	realPub := filepath.Join(tmpDir, "actual_key.pub")
	if err := os.WriteFile(realPub, []byte("pubkey"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realPub, filepath.Join(sshDir, "my-key-link.pub")); err != nil {
		t.Fatal(err)
	}

	ctx := &seatbelt.Context{HomeDir: tmpDir}
	g := guards.SSHKeysGuard()
	result := g.Rules(ctx)

	// The non-allowlisted symlink should be denied
	if len(result.Protected) != 1 {
		t.Errorf("expected 1 protected (symlink to key), got %d: %v", len(result.Protected), result.Protected)
	}
	expectedDenied := filepath.Join(sshDir, "my-key-link")
	if len(result.Protected) > 0 && result.Protected[0] != expectedDenied {
		t.Errorf("expected protected path %q, got %q", expectedDenied, result.Protected[0])
	}

	// The allowlisted symlink (.pub) should be allowed
	if len(result.Allowed) != 1 {
		t.Errorf("expected 1 allowed (symlink to .pub), got %d: %v", len(result.Allowed), result.Allowed)
	}
	expectedAllowed := filepath.Join(sshDir, "my-key-link.pub")
	if len(result.Allowed) > 0 && result.Allowed[0] != expectedAllowed {
		t.Errorf("expected allowed path %q, got %q", expectedAllowed, result.Allowed[0])
	}
}
