//go:build linux && integration

package sandbox

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/config"
)

// These tests require bwrap to be available.
// Run with: go test -tags integration ./internal/sandbox/ -v

func skipIfNoBwrap(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("bwrap"); err != nil {
		t.Skip("bwrap not on PATH -- skipping integration test")
	}
	// Check if bwrap can actually create namespaces (fails in unprivileged containers)
	cmd := exec.Command("bwrap", "--unshare-user", "--uid", "0", "--gid", "0",
		"--ro-bind", "/usr", "/usr", "--ro-bind", "/bin", "/bin",
		"--proc", "/proc", "--dev", "/dev", "--symlink", "usr/lib", "/lib",
		"--", "/bin/true")
	if err := cmd.Run(); err != nil {
		t.Skip("bwrap cannot create namespaces (unprivileged container?) -- skipping integration test")
	}
}

func TestLinuxIntegration_BwrapDeniedPathBlocked(t *testing.T) {
	skipIfNoBwrap(t)

	deniedDir := t.TempDir()
	secretFile := filepath.Join(deniedDir, "secret.txt")
	if err := os.WriteFile(secretFile, []byte("TOP SECRET"), 0600); err != nil {
		t.Fatalf("failed to create secret: %v", err)
	}

	writableDir := t.TempDir()

	policy := Policy{
		ProjectRoot:     writableDir,
		ExtraDenied:     []string{deniedDir},
		Network:         NetworkNone,
		AllowSubprocess: true,
	}

	cmd := exec.Command("/bin/cat", secretFile)
	cmd.Env = os.Environ()

	s := &LinuxSandbox{}
	bwrapPath, _ := exec.LookPath("bwrap")
	if err := s.applyBwrap(cmd, policy, bwrapPath); err != nil {
		t.Fatalf("applyBwrap failed: %v", err)
	}

	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected cat to fail on denied path, but succeeded with: %s", output)
	}
	t.Logf("bwrap correctly blocked read of denied path; error: %v, output: %s", err, output)
}

func TestLinuxIntegration_BwrapWritablePath(t *testing.T) {
	skipIfNoBwrap(t)

	writableDir := t.TempDir()

	policy := Policy{
		ProjectRoot:     writableDir,
		Network:         NetworkNone,
		AllowSubprocess: true,
	}

	targetFile := filepath.Join(writableDir, "test.txt")
	cmd := exec.Command("/usr/bin/touch", targetFile)
	cmd.Env = os.Environ()

	s := &LinuxSandbox{}
	bwrapPath, _ := exec.LookPath("bwrap")
	if err := s.applyBwrap(cmd, policy, bwrapPath); err != nil {
		t.Fatalf("applyBwrap failed: %v", err)
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected touch to succeed on writable path, but failed: %v, output: %s", err, output)
	}

	if _, err := os.Stat(targetFile); os.IsNotExist(err) {
		t.Error("expected file to exist after touch in writable dir")
	}
}

func TestLinuxIntegration_MinimalPolicyExecEcho(t *testing.T) {
	skipIfNoBwrap(t)

	runtimeDir := t.TempDir()
	writableDir := t.TempDir()

	policy := Policy{
		ProjectRoot:     writableDir,
		RuntimeDir:      runtimeDir,
		Network:         NetworkNone,
		AllowSubprocess: true,
	}

	cmd := exec.Command("/bin/echo", "sandbox works!")
	cmd.Env = os.Environ()

	s := &LinuxSandbox{}
	bwrapPath, _ := exec.LookPath("bwrap")
	if err := s.applyBwrap(cmd, policy, bwrapPath); err != nil {
		t.Fatalf("applyBwrap failed: %v", err)
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("echo failed under policy: %v, output: %s", err, output)
	}

	if !strings.Contains(string(output), "sandbox works!") {
		t.Errorf("unexpected output: %s", output)
	}
}

func TestLinuxIntegration_SandboxRefResolution(t *testing.T) {
	skipIfNoBwrap(t)

	// Test 1: nil ref -> default policy (non-nil)
	cfg, disabled, err := ResolveSandboxRef(nil, nil)
	if err != nil {
		t.Fatalf("ResolveSandboxRef(nil): %v", err)
	}
	if disabled {
		t.Fatal("nil ref should not be disabled")
	}
	if cfg != nil {
		t.Fatal("expected nil config for nil ref (use defaults)")
	}

	// Test 2: inline ref
	inlineRef := &config.SandboxRef{
		Inline: &config.SandboxPolicy{
			Writable: []string{"/custom"},
		},
	}
	cfg2, disabled2, err2 := ResolveSandboxRef(inlineRef, nil)
	if err2 != nil {
		t.Fatalf("ResolveSandboxRef(inline): %v", err2)
	}
	if disabled2 {
		t.Fatal("inline ref should not be disabled")
	}
	if cfg2 == nil || len(cfg2.Writable) != 1 {
		t.Fatalf("expected inline policy with 1 writable, got %v", cfg2)
	}

	// Test 3: execute echo through bwrap with a minimal policy
	runtimeDir := t.TempDir()
	policy := Policy{
		RuntimeDir:      runtimeDir,
		Network:         NetworkNone,
		AllowSubprocess: true,
	}

	cmd := exec.Command("/bin/echo", "ref resolution works")
	cmd.Env = os.Environ()

	s := &LinuxSandbox{}
	bwrapPath, _ := exec.LookPath("bwrap")
	if err := s.applyBwrap(cmd, policy, bwrapPath); err != nil {
		t.Fatalf("applyBwrap failed: %v", err)
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("echo failed: %v, output: %s", err, output)
	}
	if !strings.Contains(string(output), "ref resolution works") {
		t.Errorf("unexpected output: %s", output)
	}
}
