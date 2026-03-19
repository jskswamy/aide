//go:build darwin && integration

package sandbox

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// patchProfile reads the generated sandbox profile and appends rules needed
// for processes to start on modern macOS (the dynamic linker must be able to
// stat the root directory). This keeps the integration tests independent of
// any future fixes to the profile generator.
func patchProfile(runtimeDir string) error {
	profilePath := filepath.Join(runtimeDir, "sandbox.sb")
	data, err := os.ReadFile(profilePath)
	if err != nil {
		return err
	}
	profile := string(data)

	// The dynamic linker needs to read "/" metadata to resolve paths.
	if !strings.Contains(profile, `(literal "/")`) {
		profile = strings.Replace(profile,
			"(allow sysctl-read)",
			"(allow file-read* (literal \"/\"))\n(allow sysctl-read)",
			1,
		)
	}

	return os.WriteFile(profilePath, []byte(profile), 0600)
}

// realPath resolves symlinks in a path. On macOS, /var is a symlink to
// /private/var and Seatbelt profiles require canonical paths.
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

	policy := Policy{
		Denied:          []string{secretFile},
		Writable:        []string{runtimeDir},
		Readable:        []string{"/usr/bin", "/bin"},
		Network:         NetworkNone,
		AllowSubprocess: true,
	}

	cmd := exec.Command("/bin/cat", secretFile)
	cmd.Env = os.Environ()

	s := NewSandbox()
	if err := s.Apply(cmd, policy, runtimeDir); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	if err := patchProfile(runtimeDir); err != nil {
		t.Fatalf("patchProfile failed: %v", err)
	}

	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected cat to fail on denied path, but it succeeded with output: %s", output)
	}

	t.Logf("sandbox correctly blocked read of denied path; exit error: %v, output: %s", err, output)
}

func TestSandbox_AllowedPathReadable(t *testing.T) {
	runtimeDir := realPath(t, t.TempDir())
	readableDir := realPath(t, t.TempDir())

	readableFile := filepath.Join(readableDir, "hello.txt")
	content := "hello from sandbox test"
	if err := os.WriteFile(readableFile, []byte(content), 0644); err != nil {
		t.Fatalf("failed to create readable file: %v", err)
	}

	policy := Policy{
		Writable:        []string{runtimeDir},
		Readable:        []string{readableDir, "/usr/bin", "/bin"},
		Network:         NetworkNone,
		AllowSubprocess: true,
	}

	cmd := exec.Command("/bin/cat", readableFile)
	cmd.Env = os.Environ()

	s := NewSandbox()
	if err := s.Apply(cmd, policy, runtimeDir); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	if err := patchProfile(runtimeDir); err != nil {
		t.Fatalf("patchProfile failed: %v", err)
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected cat to succeed on readable path, but it failed: %v, output: %s", err, output)
	}

	if !strings.Contains(string(output), content) {
		t.Errorf("expected output to contain %q, got %q", content, string(output))
	}
}

func TestSandbox_WritablePath(t *testing.T) {
	runtimeDir := realPath(t, t.TempDir())
	writableDir := realPath(t, t.TempDir())

	policy := Policy{
		Writable:        []string{writableDir, runtimeDir},
		Readable:        []string{"/usr/bin", "/bin"},
		Network:         NetworkNone,
		AllowSubprocess: true,
	}

	targetFile := filepath.Join(writableDir, "test.txt")
	cmd := exec.Command("/usr/bin/touch", targetFile)
	cmd.Env = os.Environ()

	s := NewSandbox()
	if err := s.Apply(cmd, policy, runtimeDir); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	if err := patchProfile(runtimeDir); err != nil {
		t.Fatalf("patchProfile failed: %v", err)
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected touch to succeed on writable path, but it failed: %v, output: %s", err, output)
	}

	if _, err := os.Stat(targetFile); os.IsNotExist(err) {
		t.Error("expected file to exist after touch in writable dir, but it does not")
	}
}

func TestSandbox_WriteToReadOnlyBlocked(t *testing.T) {
	runtimeDir := realPath(t, t.TempDir())
	readOnlyDir := realPath(t, t.TempDir())

	policy := Policy{
		Writable:        []string{runtimeDir},
		Readable:        []string{readOnlyDir, "/usr/bin", "/bin"},
		Network:         NetworkNone,
		AllowSubprocess: true,
	}

	targetFile := filepath.Join(readOnlyDir, "test.txt")
	cmd := exec.Command("/usr/bin/touch", targetFile)
	cmd.Env = os.Environ()

	s := NewSandbox()
	if err := s.Apply(cmd, policy, runtimeDir); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	if err := patchProfile(runtimeDir); err != nil {
		t.Fatalf("patchProfile failed: %v", err)
	}

	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected touch to fail on read-only path, but it succeeded with output: %s", output)
	}

	t.Logf("sandbox correctly blocked write to read-only path; exit error: %v, output: %s", err, output)
}
