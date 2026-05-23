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

// TestLinuxIntegration_AtomicOverlay_EndToEnd exercises the new OverlayFS
// design end-to-end: the agent writes via atomic-rename in the sandbox, the
// sync-back step copies upper→host on exit, and a non-declared sibling on
// the host is invisible inside the sandbox.
func TestLinuxIntegration_AtomicOverlay_EndToEnd(t *testing.T) {
	skipIfNoBwrapMount(t)

	homeDir := t.TempDir()
	target := filepath.Join(homeDir, ".claude.json")
	if err := os.WriteFile(target, []byte(`{"version":1}`), 0o600); err != nil {
		t.Fatalf("setup target: %v", err)
	}
	secret := filepath.Join(homeDir, ".ssh-private-key")
	if err := os.WriteFile(secret, []byte("HOST-ONLY"), 0o600); err != nil {
		t.Fatalf("setup secret: %v", err)
	}

	runtimeDir := t.TempDir()
	layout, err := setupOverlayLayout(runtimeDir, homeDir, []string{target}, nil)
	if err != nil {
		t.Fatalf("setupOverlayLayout: %v", err)
	}
	overlayArgs := buildOverlayBwrapArgs(layout, homeDir, []string{target}, nil, nil, true, NetworkOutbound)

	// Inside the sandbox, attempt atomic-rename of .claude.json and try to
	// observe the secret. Expected: rename succeeds, secret invisible.
	script := `set -eu
ls ` + homeDir + ` > /tmp/listing
echo '{"version":2,"updated":true}' > ` + target + `.tmp
mv ` + target + `.tmp ` + target + `
cat /tmp/listing
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
	out, runErr := cmd.CombinedOutput()
	if runErr != nil {
		t.Fatalf("bwrap overlay run failed: %v\noutput:\n%s", runErr, out)
	}
	if strings.Contains(string(out), ".ssh-private-key") {
		t.Errorf("sandbox saw a non-declared sibling in $HOME; output:\n%s", out)
	}

	// Sync-back step (what __sandbox-sync would run after agent exit).
	if err := syncOverlayFile(layout.Upper, homeDir, target); err != nil {
		t.Fatalf("syncOverlayFile: %v", err)
	}
	got, err := os.ReadFile(target) // #nosec G304 -- test path under TempDir
	if err != nil {
		t.Fatalf("read host target after sync: %v", err)
	}
	if !strings.Contains(string(got), `"updated":true`) {
		t.Errorf("atomic rename did not persist via sync-back; host file: %q", got)
	}
	stillSecret, err := os.ReadFile(secret) // #nosec G304 -- test path under TempDir
	if err != nil {
		t.Fatalf("read host secret after sandbox: %v", err)
	}
	if string(stillSecret) != "HOST-ONLY" {
		t.Errorf("secret was modified through the sandbox; got: %q want HOST-ONLY", stillSecret)
	}
}

func TestLinuxIntegration_AtomicOverlay_MissingFileRefusesToLaunch(t *testing.T) {
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
}

// TestLinuxIntegration_AllowSubprocessFalse_Bwrap_BlocksFork asserts that the
// bwrap-only fallback honours AllowSubprocess=false by blocking subprocess
// creation outright, not merely isolating it via a PID namespace. The shell
// inside the sandbox attempts to spawn /bin/true; the seccomp filter we install
// must block the underlying clone()-without-CLONE_THREAD so the shell's `&&`
// branch never runs and the `||` branch prints FORK_BLOCKED instead.
func TestLinuxIntegration_AllowSubprocessFalse_Bwrap_BlocksFork(t *testing.T) {
	skipIfNoBwrap(t)

	bwrapPath, _ := exec.LookPath("bwrap")
	s := &LinuxSandbox{}
	policy := Policy{
		ProjectRoot:     t.TempDir(),
		RuntimeDir:      t.TempDir(),
		TempDir:         "/tmp",
		Network:         NetworkOutbound,
		AllowSubprocess: false,
	}

	cmd := exec.Command("/bin/sh", "-c",
		`/bin/true 2>/dev/null && echo FORK_WORKED || echo FORK_BLOCKED`)
	if err := s.applyBwrap(cmd, policy, bwrapPath); err != nil {
		t.Fatalf("applyBwrap: %v", err)
	}

	out, _ := cmd.CombinedOutput()
	output := string(out)
	if strings.Contains(output, "FORK_WORKED") {
		t.Errorf("AllowSubprocess=false should block subprocess creation; output: %s", output)
	}
	if !strings.Contains(output, "FORK_BLOCKED") {
		t.Errorf("expected FORK_BLOCKED in output; got: %s", output)
	}
}

// TestLinuxIntegration_AllowSubprocessFalse_BwrapHasUnsharePid asserts the
// bwrap fallback also adds --unshare-pid for defence in depth. The PID
// namespace alone does not block fork; the seccomp filter does. But the
// namespace bounds the blast radius if seccomp is ever bypassed.
func TestLinuxIntegration_AllowSubprocessFalse_BwrapHasUnsharePid(t *testing.T) {
	skipIfNoBwrap(t)

	bwrapPath, _ := exec.LookPath("bwrap")
	s := &LinuxSandbox{}
	policy := Policy{
		ProjectRoot:     t.TempDir(),
		RuntimeDir:      t.TempDir(),
		TempDir:         "/tmp",
		Network:         NetworkOutbound,
		AllowSubprocess: false,
	}
	cmd := exec.Command("/bin/sh", "-c",
		`echo PPID=$(cut -d' ' -f4 /proc/self/stat)`)
	if err := s.applyBwrap(cmd, policy, bwrapPath); err != nil {
		t.Fatalf("applyBwrap: %v", err)
	}
	out, _ := cmd.CombinedOutput()
	if !strings.Contains(string(out), "PPID=0") {
		t.Errorf("expected sandboxed process to see PPID=0 (PID 1 in new PID namespace); got: %s", out)
	}
}

// Note: the --unshare-pid coverage for the overlay path moved to
// TestBuildOverlayBwrapArgs_UnsharePidAndNetwork in atomic_overlay_test.go,
// which exercises the new buildOverlayBwrapArgs directly (no bwrap launch
// needed, so runs on any Linux).

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
