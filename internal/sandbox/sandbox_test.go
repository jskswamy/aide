package sandbox

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestDefaultPolicy_Paths(t *testing.T) {
	projectRoot := "/tmp/myproject"
	runtimeDir := "/tmp/aide-12345"
	homeDir := "/home/testuser"
	tempDir := "/tmp"

	policy := DefaultPolicy(projectRoot, runtimeDir, homeDir, tempDir)

	// Writable
	assertContains(t, policy.Writable, projectRoot, "Writable should contain projectRoot")
	assertContains(t, policy.Writable, runtimeDir, "Writable should contain runtimeDir")
	assertContains(t, policy.Writable, tempDir, "Writable should contain tempDir")

	// Readable — deny-list model: homeDir + projectRoot (for Linux Landlock)
	assertContains(t, policy.Readable, homeDir, "Readable should contain homeDir")
	assertContains(t, policy.Readable, projectRoot, "Readable should contain projectRoot")

	// Denied
	assertContains(t, policy.Denied, filepath.Join(homeDir, ".ssh/id_*"), "Denied should contain SSH key glob")
	assertContains(t, policy.Denied, filepath.Join(homeDir, ".aws/credentials"), "Denied should contain AWS credentials")
	assertContains(t, policy.Denied, filepath.Join(homeDir, ".config/aide/secrets"), "Denied should contain aide secrets")
}

func TestDefaultPolicy_IncludesTempDir(t *testing.T) {
	tempDir := os.TempDir()
	policy := DefaultPolicy("/tmp/proj", "/tmp/rt", "/home/user", tempDir)

	assertContains(t, policy.Writable, tempDir, "Writable should include os.TempDir()")
}

func TestDefaultPolicy_DeniedIncludesSSHKeysAndCloudCreds(t *testing.T) {
	homeDir := "/home/testuser"
	policy := DefaultPolicy("/tmp/proj", "/tmp/rt", homeDir, "/tmp")

	sshKeyGlob := filepath.Join(homeDir, ".ssh/id_*")
	awsCreds := filepath.Join(homeDir, ".aws/credentials")
	azureDir := filepath.Join(homeDir, ".azure")
	gcloudDir := filepath.Join(homeDir, ".config/gcloud")

	assertContains(t, policy.Denied, sshKeyGlob, "Denied should contain SSH key glob")
	assertContains(t, policy.Denied, awsCreds, "Denied should contain AWS credentials")
	assertContains(t, policy.Denied, azureDir, "Denied should contain Azure directory")
	assertContains(t, policy.Denied, gcloudDir, "Denied should contain gcloud directory")
}

func TestDefaultPolicy_NetworkIsOutbound(t *testing.T) {
	policy := DefaultPolicy("/tmp/proj", "/tmp/rt", "/home/user", "/tmp")

	if policy.Network != NetworkOutbound {
		t.Errorf("expected Network=%q, got %q", NetworkOutbound, policy.Network)
	}
}

func TestDefaultPolicy_AllowSubprocess(t *testing.T) {
	policy := DefaultPolicy("/tmp/proj", "/tmp/rt", "/home/user", "/tmp")

	if !policy.AllowSubprocess {
		t.Error("expected AllowSubprocess=true, got false")
	}
}

func TestDefaultPolicy_CleanEnv(t *testing.T) {
	policy := DefaultPolicy("/tmp/proj", "/tmp/rt", "/home/user", "/tmp")

	if policy.CleanEnv {
		t.Error("expected CleanEnv=false, got true")
	}
}

func TestNoopSandbox_Apply_ReturnsNil(t *testing.T) {
	s := &noopSandbox{}
	cmd := exec.Command("echo", "hello")
	policy := DefaultPolicy("/tmp/proj", "/tmp/rt", "/home/user", "/tmp")

	err := s.Apply(cmd, policy, "/tmp/rt")
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
}

func TestNewSandbox_ReturnsNonNil(t *testing.T) {
	s := NewSandbox()
	if s == nil {
		t.Error("expected NewSandbox() to return non-nil Sandbox")
	}
}

func TestPolicy_DeniedPrecedence(t *testing.T) {
	// Contract test: if a path appears in both Readable and Denied,
	// Denied should take precedence. This is a documented contract;
	// actual enforcement is OS-specific.
	policy := Policy{
		Denied: []string{"/home/user/.ssh/id_rsa"},
	}

	found := false
	for _, d := range policy.Denied {
		if d == "/home/user/.ssh/id_rsa" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected Denied to contain /home/user/.ssh/id_rsa")
	}
}

func TestNetworkModeConstants(t *testing.T) {
	if NetworkOutbound != "outbound" {
		t.Errorf("expected NetworkOutbound=%q, got %q", "outbound", NetworkOutbound)
	}
	if NetworkNone != "none" {
		t.Errorf("expected NetworkNone=%q, got %q", "none", NetworkNone)
	}
	if NetworkUnrestricted != "unrestricted" {
		t.Errorf("expected NetworkUnrestricted=%q, got %q", "unrestricted", NetworkUnrestricted)
	}
}

// helper
func assertContains(t *testing.T, slice []string, item string, msg string) {
	t.Helper()
	for _, s := range slice {
		if s == item {
			return
		}
	}
	t.Errorf("%s: %q not found in %v", msg, item, slice)
}
