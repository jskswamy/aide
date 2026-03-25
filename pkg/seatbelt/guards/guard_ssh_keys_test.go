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

	g := guards.SSHKeysGuard()
	result := g.Rules(ctx)

	// Should still have agent socket deny rules even without ~/.ssh
	output := renderTestRules(result.Rules)
	if !strings.Contains(output, `/tmp/ssh-`) {
		t.Error("expected agent socket deny rules even when ~/.ssh missing")
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

	// Should have metadata rule for .ssh dir but no deny rules
	output := renderTestRules(result.Rules)
	if !strings.Contains(output, "file-read-metadata") {
		t.Error("expected metadata rule for .ssh directory")
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

	// Safe files no longer generate allow rules (covered by filesystem guard's ~/.ssh subpath)
	output := renderTestRules(result.Rules)
	if strings.Contains(output, "allow file-read*") && strings.Contains(output, ".pub") {
		t.Error("safe files should NOT have individual allow rules (covered by filesystem guard)")
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

	// Safe files no longer have individual allow rules (covered by filesystem guard)
	// But verify they are tracked in result.Allowed
	for _, name := range safeFiles {
		fullPath := filepath.Join(sshDir, name)
		found := false
		for _, a := range result.Allowed {
			if a == fullPath {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected %s in result.Allowed", fullPath)
		}
	}

	// Verify metadata allow for .ssh directory itself
	if !strings.Contains(output, fmt.Sprintf(`(allow file-read-metadata (literal "%s"))`, sshDir)) {
		t.Error("expected metadata allow rule for .ssh directory")
	}
}

func TestSSHKeysGuard_DeniesSSHAuthSockSocket(t *testing.T) {
	tmpDir := t.TempDir()
	sshDir := filepath.Join(tmpDir, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}

	// Simulate SSH_AUTH_SOCK pointing to a GPG agent socket
	agentSock := filepath.Join(tmpDir, ".gnupg", "S.gpg-agent.ssh")
	if err := os.MkdirAll(filepath.Dir(agentSock), 0o700); err != nil {
		t.Fatal(err)
	}

	ctx := &seatbelt.Context{
		HomeDir: tmpDir,
		Env:     []string{"SSH_AUTH_SOCK=" + agentSock},
	}
	g := guards.SSHKeysGuard()
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	// Should deny network-outbound to the SSH_AUTH_SOCK unix socket
	if !strings.Contains(output, fmt.Sprintf(`(deny network-outbound (remote unix-socket (path-literal "%s")))`, agentSock)) {
		t.Errorf("expected deny network-outbound unix-socket for SSH_AUTH_SOCK path %s\ngot:\n%s", agentSock, output)
	}
}

func TestSSHKeysGuard_DeniesGPGAgentSSHSocket(t *testing.T) {
	tmpDir := t.TempDir()
	sshDir := filepath.Join(tmpDir, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}

	// GPG agent SSH socket exists but SSH_AUTH_SOCK is not set
	gpgDir := filepath.Join(tmpDir, ".gnupg")
	if err := os.MkdirAll(gpgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	agentSock := filepath.Join(gpgDir, "S.gpg-agent.ssh")
	if err := os.WriteFile(agentSock, []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx := &seatbelt.Context{HomeDir: tmpDir}
	g := guards.SSHKeysGuard()
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	// Should deny network-outbound to ~/.gnupg/S.gpg-agent.ssh even without SSH_AUTH_SOCK
	if !strings.Contains(output, fmt.Sprintf(`(deny network-outbound (remote unix-socket (path-literal "%s")))`, agentSock)) {
		t.Errorf("expected deny network-outbound unix-socket for GPG agent SSH socket %s\ngot:\n%s", agentSock, output)
	}
}

func TestSSHKeysGuard_DeniesStandardSSHAgentSockets(t *testing.T) {
	tmpDir := t.TempDir()
	sshDir := filepath.Join(tmpDir, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}

	ctx := &seatbelt.Context{HomeDir: tmpDir}
	g := guards.SSHKeysGuard()
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	// Should deny network-outbound to standard ssh-agent socket patterns in /tmp
	if !strings.Contains(output, `deny network-outbound`) || !strings.Contains(output, `/tmp/ssh-`) {
		t.Error("expected deny network-outbound regex rule for /tmp/ssh-*/agent.* sockets")
	}
	if !strings.Contains(output, `/private/tmp/ssh-`) {
		t.Error("expected deny network-outbound regex rule for /private/tmp/ssh-*/agent.* sockets")
	}
}

func TestSSHKeysGuard_SSHAuthSockNotSet(t *testing.T) {
	tmpDir := t.TempDir()
	sshDir := filepath.Join(tmpDir, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}

	// No SSH_AUTH_SOCK in env, no GPG agent socket file exists
	ctx := &seatbelt.Context{HomeDir: tmpDir}
	g := guards.SSHKeysGuard()
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	// Should still have the regex rules for /tmp/ssh-* patterns
	if !strings.Contains(output, `/tmp/ssh-`) {
		t.Error("expected deny regex for /tmp/ssh-* even without SSH_AUTH_SOCK")
	}
	// Should NOT have a literal deny for an empty SSH_AUTH_SOCK
	if strings.Contains(output, `(deny network-outbound (remote unix-socket (path-literal "")))`) {
		t.Error("should not generate deny rule for empty SSH_AUTH_SOCK")
	}
}

func TestSSHKeysGuard_DeduplicatesSSHAuthSockAndGPGSocket(t *testing.T) {
	tmpDir := t.TempDir()
	sshDir := filepath.Join(tmpDir, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}

	// SSH_AUTH_SOCK points to the same path as ~/.gnupg/S.gpg-agent.ssh
	gpgDir := filepath.Join(tmpDir, ".gnupg")
	if err := os.MkdirAll(gpgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	agentSock := filepath.Join(gpgDir, "S.gpg-agent.ssh")
	if err := os.WriteFile(agentSock, []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx := &seatbelt.Context{
		HomeDir: tmpDir,
		Env:     []string{"SSH_AUTH_SOCK=" + agentSock},
	}
	g := guards.SSHKeysGuard()
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	// Should only have one deny for this path, not two
	count := strings.Count(output, fmt.Sprintf(`(deny network-outbound (remote unix-socket (path-literal "%s")))`, agentSock))
	if count != 1 {
		t.Errorf("expected 1 deny network-outbound for %s, got %d", agentSock, count)
	}
}

func TestSSHKeysGuard_DeniesAgentSocketEvenWithoutSSHDir(t *testing.T) {
	tmpDir := t.TempDir()
	// No .ssh directory exists

	agentSock := filepath.Join(tmpDir, ".gnupg", "S.gpg-agent.ssh")
	if err := os.MkdirAll(filepath.Dir(agentSock), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(agentSock, []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx := &seatbelt.Context{
		HomeDir: tmpDir,
		Env:     []string{"SSH_AUTH_SOCK=" + agentSock},
	}
	g := guards.SSHKeysGuard()
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	// Agent socket denial should work even without ~/.ssh
	if !strings.Contains(output, fmt.Sprintf(`(deny network-outbound (remote unix-socket (path-literal "%s")))`, agentSock)) {
		t.Error("expected agent socket deny even when ~/.ssh doesn't exist")
	}
	if !strings.Contains(output, `/tmp/ssh-`) {
		t.Error("expected /tmp/ssh-* regex deny even when ~/.ssh doesn't exist")
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

	if len(result.Protected) != 1 {
		t.Errorf("expected 1 protected (symlink to key), got %d: %v", len(result.Protected), result.Protected)
	}
	expectedDenied := filepath.Join(sshDir, "my-key-link")
	if len(result.Protected) > 0 && result.Protected[0] != expectedDenied {
		t.Errorf("expected protected path %q, got %q", expectedDenied, result.Protected[0])
	}

	if len(result.Allowed) != 1 {
		t.Errorf("expected 1 allowed (symlink to .pub), got %d: %v", len(result.Allowed), result.Allowed)
	}
	expectedAllowed := filepath.Join(sshDir, "my-key-link.pub")
	if len(result.Allowed) > 0 && result.Allowed[0] != expectedAllowed {
		t.Errorf("expected allowed path %q, got %q", expectedAllowed, result.Allowed[0])
	}
}
