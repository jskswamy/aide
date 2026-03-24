//go:build darwin && integration

package sandbox

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func dirExistsInteg(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func TestNixGuard_StatNix(t *testing.T) {
	if !dirExistsInteg("/nix/store") {
		t.Skip("nix not installed")
	}

	runtimeDir := realPath(t, t.TempDir())
	projectDir := realPath(t, t.TempDir())
	policy := DefaultPolicy(projectDir, runtimeDir, os.TempDir(), os.Environ())

	cmd := exec.Command("/usr/bin/stat", "/nix")
	cmd.Env = os.Environ()

	s := NewSandbox()
	if err := s.Apply(cmd, policy, runtimeDir); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("stat /nix failed inside sandbox: %v\noutput: %s", err, output)
	}
}

func TestNixGuard_StatRunCurrentSystem(t *testing.T) {
	if !dirExistsInteg("/nix/store") {
		t.Skip("nix not installed")
	}

	runtimeDir := realPath(t, t.TempDir())
	projectDir := realPath(t, t.TempDir())
	policy := DefaultPolicy(projectDir, runtimeDir, os.TempDir(), os.Environ())

	cmd := exec.Command("/usr/bin/stat", "/private/var/run/current-system")
	cmd.Env = os.Environ()

	s := NewSandbox()
	if err := s.Apply(cmd, policy, runtimeDir); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("stat /private/var/run/current-system failed inside sandbox: %v\noutput: %s", err, output)
	}
}

func TestNixGuard_GoToolchain(t *testing.T) {
	if !dirExistsInteg("/nix/store") {
		t.Skip("nix not installed")
	}

	goPath := filepath.Join(os.Getenv("HOME"), ".nix-profile", "bin", "go")
	if _, err := os.Stat(goPath); os.IsNotExist(err) {
		t.Skipf("nix go not found at %s", goPath)
	}

	runtimeDir := realPath(t, t.TempDir())
	projectDir := realPath(t, t.TempDir())
	policy := DefaultPolicy(projectDir, runtimeDir, os.TempDir(), os.Environ())

	cmd := exec.Command(goPath, "env", "GOROOT")
	cmd.Env = os.Environ()

	s := NewSandbox()
	if err := s.Apply(cmd, policy, runtimeDir); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go env GOROOT failed inside sandbox: %v\noutput: %s", err, output)
	}

	if !strings.Contains(string(output), "/nix/store") {
		t.Errorf("expected GOROOT in /nix/store, got: %s", output)
	}
}
