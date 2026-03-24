//go:build darwin && integration

package sandbox

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func realPath(t *testing.T, p string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", p, err)
	}
	return resolved
}

func TestSandbox_DeniedPathBlocked(t *testing.T) {
	runtimeDir := realPath(t, t.TempDir())
	deniedDir := realPath(t, t.TempDir())

	secretFile := filepath.Join(deniedDir, "id_rsa")
	if err := os.WriteFile(secretFile, []byte("TOP SECRET KEY"), 0600); err != nil {
		t.Fatalf("failed to create secret file: %v", err)
	}

	policy := DefaultPolicy(deniedDir, runtimeDir, os.TempDir(), os.Environ())
	policy.ExtraDenied = []string{secretFile}

	cmd := exec.Command("/bin/cat", secretFile)
	cmd.Env = os.Environ()

	s := NewSandbox()
	if err := s.Apply(cmd, policy, runtimeDir); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected cat to fail on denied path, but it succeeded with output: %s", output)
	}
	t.Logf("sandbox correctly blocked read of denied path; exit error: %v, output: %s", err, output)
}

func TestSandbox_AllowedPathReadable(t *testing.T) {
	runtimeDir := realPath(t, t.TempDir())
	projectDir := realPath(t, t.TempDir())

	readableFile := filepath.Join(projectDir, "hello.txt")
	content := "hello from sandbox test"
	if err := os.WriteFile(readableFile, []byte(content), 0644); err != nil {
		t.Fatalf("failed to create readable file: %v", err)
	}

	policy := DefaultPolicy(projectDir, runtimeDir, os.TempDir(), os.Environ())

	cmd := exec.Command("/bin/cat", readableFile)
	cmd.Env = os.Environ()

	s := NewSandbox()
	if err := s.Apply(cmd, policy, runtimeDir); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected cat to succeed on project path, but it failed: %v, output: %s", err, output)
	}
	if !strings.Contains(string(output), content) {
		t.Errorf("expected output to contain %q, got %q", content, string(output))
	}
}

func TestSandbox_WritablePath(t *testing.T) {
	runtimeDir := realPath(t, t.TempDir())
	projectDir := realPath(t, t.TempDir())

	policy := DefaultPolicy(projectDir, runtimeDir, os.TempDir(), os.Environ())

	targetFile := filepath.Join(projectDir, "test.txt")
	cmd := exec.Command("/usr/bin/touch", targetFile)
	cmd.Env = os.Environ()

	s := NewSandbox()
	if err := s.Apply(cmd, policy, runtimeDir); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected touch to succeed on project path, but it failed: %v, output: %s", err, output)
	}
	if _, err := os.Stat(targetFile); os.IsNotExist(err) {
		t.Error("expected file to exist after touch in project dir, but it does not")
	}
}

func TestSandbox_ExtraWritablePath(t *testing.T) {
	runtimeDir := realPath(t, t.TempDir())
	projectDir := realPath(t, t.TempDir())
	extraDir := realPath(t, t.TempDir())

	policy := DefaultPolicy(projectDir, runtimeDir, os.TempDir(), os.Environ())
	policy.ExtraWritable = []string{extraDir}

	targetFile := filepath.Join(extraDir, "extra.txt")
	cmd := exec.Command("/usr/bin/touch", targetFile)
	cmd.Env = os.Environ()

	s := NewSandbox()
	if err := s.Apply(cmd, policy, runtimeDir); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected touch to succeed on extra writable path, but it failed: %v, output: %s", err, output)
	}
	if _, err := os.Stat(targetFile); os.IsNotExist(err) {
		t.Error("expected file to exist after touch in extra writable dir")
	}
}

func TestSandbox_WriteToReadOnlyBlocked(t *testing.T) {
	// Use a directory under $HOME (not in scoped reads) so that ExtraReadable
	// is the only grant.  Temp dirs resolve under /private/var/folders/ which
	// already has file-read*/file-write* from system-runtime temp rules, so
	// the write-block test would pass incorrectly with a temp path.
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	readOnlyDir, err := os.MkdirTemp(home, "test-readonly-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(readOnlyDir)
	readOnlyDir = realPath(t, readOnlyDir)

	runtimeDir := realPath(t, t.TempDir())
	projectDir := realPath(t, t.TempDir())

	policy := DefaultPolicy(projectDir, runtimeDir, os.TempDir(), os.Environ())
	policy.ExtraReadable = []string{readOnlyDir}

	targetFile := filepath.Join(readOnlyDir, "test.txt")
	cmd := exec.Command("/usr/bin/touch", targetFile)
	cmd.Env = os.Environ()

	s := NewSandbox()
	if err := s.Apply(cmd, policy, runtimeDir); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected touch to fail on read-only path, but it succeeded with output: %s", output)
	}
	t.Logf("sandbox correctly blocked write to read-only path; exit error: %v, output: %s", err, output)
}

func TestSandbox_HomeDocumentsNotReadable(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}

	blockedDir := filepath.Join(home, "sandbox-test-blocked")
	if err := os.MkdirAll(blockedDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	defer os.RemoveAll(blockedDir)

	testFile := filepath.Join(blockedDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("secret content"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	testFile = realPath(t, testFile)

	runtimeDir := realPath(t, t.TempDir())
	projectDir := realPath(t, t.TempDir())
	policy := DefaultPolicy(projectDir, runtimeDir, os.TempDir(), os.Environ())

	cmd := exec.Command("/bin/cat", testFile)
	cmd.Env = os.Environ()

	s := NewSandbox()
	if err := s.Apply(cmd, policy, runtimeDir); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected cat to fail on non-scoped home path, but it succeeded with output: %s", output)
	}
	t.Logf("sandbox correctly blocked read of non-scoped home path; exit error: %v, output: %s", err, output)
}

func TestSandbox_HomeDotfileReadable(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}

	dotfile := filepath.Join(home, ".sandbox-test-dotfile")
	content := "dotfile readable content"
	if err := os.WriteFile(dotfile, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	defer os.Remove(dotfile)
	dotfile = realPath(t, dotfile)

	runtimeDir := realPath(t, t.TempDir())
	projectDir := realPath(t, t.TempDir())
	policy := DefaultPolicy(projectDir, runtimeDir, os.TempDir(), os.Environ())

	cmd := exec.Command("/bin/cat", dotfile)
	cmd.Env = os.Environ()

	s := NewSandbox()
	if err := s.Apply(cmd, policy, runtimeDir); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected cat to succeed on home dotfile, but it failed: %v, output: %s", err, output)
	}
	if !strings.Contains(string(output), content) {
		t.Errorf("expected output to contain %q, got %q", content, string(output))
	}
}
