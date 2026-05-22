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

// These tests require bwrap to be available (e.g. in the devcontainer).
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

// skipIfNoBwrapMount probes only mount-namespace + tmpfs/bind support, so tests
// run on hosts where unprivileged user-namespace creation is blocked.
func skipIfNoBwrapMount(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("bwrap"); err != nil {
		t.Skip("bwrap not on PATH -- skipping integration test")
	}
	cmd := exec.Command("bwrap",
		"--bind", "/", "/", "--proc", "/proc", "--dev", "/dev",
		"--", "/bin/true")
	if err := cmd.Run(); err != nil {
		t.Skipf("bwrap cannot create a mount namespace on this host: %v -- skipping integration test", err)
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

// TestLinuxIntegration_DefaultPolicy_SSHNotReadable verifies that with the default policy
// (no extra capabilities), the agent cannot read ~/.ssh.
func TestLinuxIntegration_DefaultPolicy_SSHNotReadable(t *testing.T) {
	skipIfNoBwrap(t)

	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home dir")
	}
	sshDir := filepath.Join(home, ".ssh")
	if _, err := os.Stat(sshDir); os.IsNotExist(err) {
		t.Skip("~/.ssh does not exist on this system")
	}

	writableDir := t.TempDir()
	runtimeDir := t.TempDir()

	policy := Policy{
		ProjectRoot:     writableDir,
		RuntimeDir:      runtimeDir,
		TempDir:         os.TempDir(),
		Network:         NetworkNone,
		AllowSubprocess: true,
		Guards:          []string{},
	}

	// Try to list ~/.ssh through the sandbox
	cmd := exec.Command("/bin/ls", sshDir)
	cmd.Env = os.Environ()

	s := &LinuxSandbox{}
	bwrapPath, _ := exec.LookPath("bwrap")
	if err := s.applyBwrap(cmd, policy, bwrapPath); err != nil {
		t.Fatalf("applyBwrap failed: %v", err)
	}

	_, err = cmd.CombinedOutput()
	if err == nil {
		t.Error("agent should NOT be able to read ~/.ssh with default policy (no coarse $HOME allow)")
	} else {
		t.Logf("correctly blocked ~/.ssh access: %v", err)
	}
}

// TestLinuxIntegration_DefaultPolicy_ProjectRootReadable verifies project root remains readable.
func TestLinuxIntegration_DefaultPolicy_ProjectRootReadable(t *testing.T) {
	skipIfNoBwrap(t)

	writableDir := t.TempDir()
	testFile := filepath.Join(writableDir, "hello.txt")
	if err := os.WriteFile(testFile, []byte("hello"), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	policy := Policy{
		ProjectRoot:     writableDir,
		RuntimeDir:      t.TempDir(),
		TempDir:         os.TempDir(),
		Network:         NetworkNone,
		AllowSubprocess: true,
		Guards:          []string{},
	}

	gps := DeriveGrantedPathSet(policy)

	cmd := exec.Command("/bin/cat", testFile)
	cmd.Env = os.Environ()

	s := &LinuxSandbox{}
	bwrapPath, _ := exec.LookPath("bwrap")
	// Build bwrap args manually using GrantedPathSet
	var bwrapArgs []string
	for _, p := range gps.Writable {
		bwrapArgs = append(bwrapArgs, "--bind", p, p)
	}
	for _, p := range gps.Readable {
		bwrapArgs = append(bwrapArgs, "--ro-bind-try", p, p)
	}
	bwrapArgs = append(bwrapArgs,
		"--ro-bind", "/usr", "/usr",
		"--ro-bind", "/etc/resolv.conf", "/etc/resolv.conf",
		"--proc", "/proc", "--dev", "/dev", "--tmpfs", "/tmp",
	)
	for _, lib := range []string{"/lib", "/lib64"} {
		if _, err := os.Stat(lib); err == nil {
			bwrapArgs = append(bwrapArgs, "--ro-bind", lib, lib)
		}
	}
	bwrapArgs = append(bwrapArgs, "--", "/bin/cat", testFile)

	cmd.Path = bwrapPath
	cmd.Args = append([]string{"bwrap"}, bwrapArgs...)
	_ = s // use s to avoid unused warning

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("project root should be readable: %v, output: %s", err, output)
	}
	if string(output) != "hello" {
		t.Errorf("unexpected output: %q, want %q", string(output), "hello")
	}
}

func TestLinuxIntegration_AtomicOverlay_InPlaceWritePersistsToHost(t *testing.T) {
	skipIfNoBwrapMount(t)

	parent := t.TempDir()
	target := filepath.Join(parent, ".claude.json")
	if err := os.WriteFile(target, []byte(`{"version":1}`), 0600); err != nil {
		t.Fatalf("setup target file: %v", err)
	}

	secret := filepath.Join(parent, "secret.txt")
	if err := os.WriteFile(secret, []byte("HOST-ONLY"), 0600); err != nil {
		t.Fatalf("setup secret: %v", err)
	}

	j := landlockPolicyJSON{AgentAtomicWritableFiles: []string{target}}
	overlayArgs := buildAtomicWriteOverlayArgs(j, GrantedPathSet{}, "", "")

	script := `
set -eu
echo "--- ls inside overlay ---"
ls ` + parent + `
echo "--- in-place rewrite of target ---"
echo '{"version":2,"updated":true}' > ` + target + `
echo "--- write sibling into overlay tmpfs ---"
echo 'OVERWRITTEN-IN-SANDBOX' > ` + secret + `
echo "--- done ---"
`

	bwrapPath, _ := exec.LookPath("bwrap")
	args := append([]string{}, overlayArgs...)
	args = append(args, "--ro-bind", "/usr", "/usr")
	for _, lib := range []string{"/lib", "/lib64"} {
		if _, err := os.Stat(lib); err == nil {
			args = append(args, "--ro-bind", lib, lib)
		}
	}
	args = append(args, "--", "/bin/sh", "-c", script)

	cmd := exec.Command(bwrapPath, args...)
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bwrap overlay run failed: %v\noutput:\n%s", err, out)
	}

	t.Logf("overlay output:\n%s", out)

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read host target after sandbox: %v", err)
	}
	if !strings.Contains(string(got), `"updated":true`) {
		t.Errorf("host target was not updated by the in-sandbox write; got: %q", got)
	}

	stillSecret, err := os.ReadFile(secret)
	if err != nil {
		t.Fatalf("read host secret after sandbox: %v", err)
	}
	if string(stillSecret) != "HOST-ONLY" {
		t.Errorf("secret.txt was modified through the overlay; got: %q want %q", stillSecret, "HOST-ONLY")
	}

	if strings.Contains(string(out), "secret.txt") {
		t.Errorf("sandbox could see secret.txt through the overlay; output:\n%s", out)
	}
}

// TestLinuxIntegration_AtomicOverlay_PreflightDetectsEBUSY asserts that the
// production preflightAtomicRename helper detects the rename-over-bind EBUSY
// constraint and returns a refusal error rather than silently allowing a
// launch where every atomic write would be lost.
func TestLinuxIntegration_AtomicOverlay_PreflightDetectsEBUSY(t *testing.T) {
	skipIfNoBwrapMount(t)

	bwrapPath, err := exec.LookPath("bwrap")
	if err != nil {
		t.Skip("bwrap not on PATH")
	}

	err = preflightAtomicRename(bwrapPath)
	if err == nil {
		t.Fatal("preflight should detect kernel EBUSY on rename-over-bind and refuse; got nil error")
	}
	if !strings.Contains(err.Error(), "atomic-rename preflight failed") {
		t.Errorf("expected 'atomic-rename preflight failed' in error, got: %v", err)
	}
}

// rename(2) over a bind-mounted target returns EBUSY on Linux. This test pins
// that constraint so a future overlay redesign (OverlayFS, mount-move) signals
// when the limitation lifts.
func TestLinuxIntegration_AtomicOverlay_RenameOverBindFileFailsWithEBUSY(t *testing.T) {
	skipIfNoBwrapMount(t)

	parent := t.TempDir()
	target := filepath.Join(parent, ".claude.json")
	if err := os.WriteFile(target, []byte(`{"v":1}`), 0600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	j := landlockPolicyJSON{AgentAtomicWritableFiles: []string{target}}
	overlayArgs := buildAtomicWriteOverlayArgs(j, GrantedPathSet{}, "", "")

	script := `
echo '{"v":2}' > ` + parent + `/.claude.json.tmp.$$
mv ` + parent + `/.claude.json.tmp.$$ ` + target + `
`

	bwrapPath, _ := exec.LookPath("bwrap")
	args := append([]string{}, overlayArgs...)
	args = append(args, "--ro-bind", "/usr", "/usr")
	for _, lib := range []string{"/lib", "/lib64"} {
		if _, err := os.Stat(lib); err == nil {
			args = append(args, "--ro-bind", lib, lib)
		}
	}
	args = append(args, "--", "/bin/sh", "-c", script)

	cmd := exec.Command(bwrapPath, args...)
	cmd.Env = os.Environ()
	out, _ := cmd.CombinedOutput()

	if !strings.Contains(string(out), "Device or resource busy") &&
		!strings.Contains(string(out), "EBUSY") {
		t.Errorf("expected mv-over-bind to fail with EBUSY (Linux kernel constraint); got output:\n%s", out)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read host target: %v", err)
	}
	if !strings.Contains(string(got), `"v":1`) {
		t.Errorf("host file was unexpectedly modified by failed rename; got: %q", got)
	}
}

func TestLinuxIntegration_AtomicOverlay_MissingFileRefusesToLaunch(t *testing.T) {
	skipIfNoBwrapMount(t)

	parent := t.TempDir()
	missing := filepath.Join(parent, ".claude.json")

	j := landlockPolicyJSON{AgentAtomicWritableFiles: []string{missing}}
	got := missingAtomicWritableFiles(j)
	if len(got) != 1 || got[0] != missing {
		t.Fatalf("missingAtomicWritableFiles should report %q as missing, got: %v", missing, got)
	}

	if !needsAtomicWriteOverlay(j) {
		t.Error("policy declares an atomic file; needsAtomicWriteOverlay must return true so applyLandlock can hit the missing-file guard")
	}

	args := buildAtomicWriteOverlayArgs(j, GrantedPathSet{}, "", "")
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "--bind "+missing) {
		t.Errorf("overlay must not --bind a non-existent file; got: %s", joined)
	}
	if !strings.Contains(joined, "--tmpfs "+filepath.Clean(parent)) {
		t.Errorf("overlay should still tmpfs the parent so the missing-file guard runs in applyLandlock; got: %s", joined)
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
