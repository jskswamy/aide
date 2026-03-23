//go:build linux

package sandbox

import (
	"bytes"
	"log"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestLinuxSandbox_NewSandbox_ReturnsLinux(t *testing.T) {
	s := NewSandbox()
	if s == nil {
		t.Fatal("NewSandbox() returned nil")
	}
	if _, ok := s.(*LinuxSandbox); !ok {
		t.Errorf("NewSandbox() returned %T, want *LinuxSandbox", s)
	}
}

func TestLinuxSandbox_LandlockAvailable(t *testing.T) {
	// Just test that the function doesn't panic.
	// Whether it returns true depends on the kernel.
	avail := landlockAvailable()
	t.Logf("Landlock available: %v", avail)
}

func TestLinuxSandbox_ApplyBwrap_BasicArgs(t *testing.T) {
	s := &LinuxSandbox{}
	cmd := exec.Command("/usr/bin/echo", "hello")
	policy := Policy{
		ProjectRoot:     "/tmp/project",
		RuntimeDir:      "/tmp/rt",
		TempDir:         "/tmp",
		Network:         NetworkOutbound,
		AllowSubprocess: true,
		CleanEnv:        false,
	}

	bwrapPath, err := exec.LookPath("bwrap")
	if err != nil {
		t.Skip("bwrap not on PATH")
	}

	err = s.applyBwrap(cmd, policy, bwrapPath)
	if err != nil {
		t.Fatalf("applyBwrap error: %v", err)
	}

	if cmd.Path != bwrapPath {
		t.Errorf("cmd.Path = %q, want %q", cmd.Path, bwrapPath)
	}

	args := strings.Join(cmd.Args, " ")

	// Check writable bind
	if !strings.Contains(args, "--bind /tmp/project /tmp/project") {
		t.Errorf("missing --bind for writable path in: %s", args)
	}

	// Check original command is after --
	if !strings.Contains(args, "-- /usr/bin/echo hello") {
		t.Errorf("original command not after -- in: %s", args)
	}
}

func TestLinuxSandbox_ApplyBwrap_NetworkNone(t *testing.T) {
	s := &LinuxSandbox{}
	cmd := exec.Command("/usr/bin/echo")
	policy := Policy{
		Network:         NetworkNone,
		AllowSubprocess: true,
	}

	bwrapPath, err := exec.LookPath("bwrap")
	if err != nil {
		t.Skip("bwrap not on PATH")
	}

	err = s.applyBwrap(cmd, policy, bwrapPath)
	if err != nil {
		t.Fatalf("applyBwrap error: %v", err)
	}

	args := strings.Join(cmd.Args, " ")
	if !strings.Contains(args, "--unshare-net") {
		t.Errorf("expected --unshare-net for NetworkNone, got: %s", args)
	}
}

func TestLinuxSandbox_ApplyBwrap_NoSubprocess(t *testing.T) {
	s := &LinuxSandbox{}
	cmd := exec.Command("/usr/bin/echo")
	policy := Policy{
		AllowSubprocess: false,
		Network:         NetworkOutbound,
	}

	bwrapPath, err := exec.LookPath("bwrap")
	if err != nil {
		t.Skip("bwrap not on PATH")
	}

	err = s.applyBwrap(cmd, policy, bwrapPath)
	if err != nil {
		t.Fatalf("applyBwrap error: %v", err)
	}

	args := strings.Join(cmd.Args, " ")
	if !strings.Contains(args, "--unshare-pid") {
		t.Errorf("expected --unshare-pid when AllowSubprocess=false, got: %s", args)
	}
}

func TestLinuxSandbox_ApplyBwrap_CleanEnv(t *testing.T) {
	s := &LinuxSandbox{}
	cmd := exec.Command("/usr/bin/echo")
	cmd.Env = []string{
		"PATH=/usr/bin",
		"HOME=/root",
		"SECRET_KEY=leaked",
		"TERM=xterm",
	}
	policy := Policy{
		CleanEnv:        true,
		Network:         NetworkOutbound,
		AllowSubprocess: true,
	}

	bwrapPath, err := exec.LookPath("bwrap")
	if err != nil {
		t.Skip("bwrap not on PATH")
	}

	err = s.applyBwrap(cmd, policy, bwrapPath)
	if err != nil {
		t.Fatalf("applyBwrap error: %v", err)
	}

	for _, e := range cmd.Env {
		if strings.HasPrefix(e, "SECRET_KEY=") {
			t.Errorf("SECRET_KEY should have been filtered out, got: %s", e)
		}
	}

	found := false
	for _, e := range cmd.Env {
		if strings.HasPrefix(e, "PATH=") {
			found = true
		}
	}
	if !found {
		t.Error("PATH should be kept in clean env")
	}
}

func TestLinuxSandbox_Apply_FallsBackGracefully(t *testing.T) {
	// When neither Landlock nor bwrap is available, Apply should not error
	s := &LinuxSandbox{}
	cmd := exec.Command("/usr/bin/echo", "test")
	runtimeDir := t.TempDir()
	policy := DefaultPolicy("/tmp/proj", runtimeDir, "/tmp", nil)

	// This test exercises the full Apply path.
	// On systems with bwrap (our devcontainer), it will use bwrap.
	// On systems with Landlock, it will use Landlock.
	// Either way, it should not error.
	err := s.Apply(cmd, policy, runtimeDir)
	if err != nil {
		t.Fatalf("Apply should not error: %v", err)
	}
}

func TestLandlock_PortFiltering_RewritesCmd(t *testing.T) {
	s := &LinuxSandbox{}
	cmd := exec.Command("/usr/bin/echo", "hello")
	policy := Policy{
		ProjectRoot:     "/tmp/project",
		RuntimeDir:      "/tmp/rt",
		TempDir:         "/tmp",
		Network:         NetworkOutbound,
		AllowPorts:      []int{443},
		AllowSubprocess: true,
	}

	runtimeDir := t.TempDir()
	err := s.applyLandlock(cmd, policy, runtimeDir)
	if err != nil {
		t.Fatalf("applyLandlock error: %v", err)
	}

	// Read the policy JSON that was written
	policyPath := runtimeDir + "/landlock-policy.json"
	policyBytes, err := os.ReadFile(policyPath)
	if err != nil {
		t.Fatalf("reading policy file: %v", err)
	}

	policyJSON := string(policyBytes)

	// Verify the policy JSON contains AllowPorts with 443
	if !strings.Contains(policyJSON, `"AllowPorts":[443]`) {
		t.Errorf("policy JSON should contain AllowPorts=[443], got: %s", policyJSON)
	}

	// Verify the command was rewritten to use aide __sandbox-apply
	args := strings.Join(cmd.Args, " ")
	if !strings.Contains(args, "__sandbox-apply") {
		t.Errorf("expected __sandbox-apply in args, got: %s", args)
	}
	if !strings.Contains(args, policyPath) {
		t.Errorf("expected policy path %s in args, got: %s", policyPath, args)
	}
}

func TestLandlock_PortFiltering_DenyPorts(t *testing.T) {
	s := &LinuxSandbox{}
	cmd := exec.Command("/usr/bin/echo", "hello")
	policy := Policy{
		ProjectRoot:     "/tmp/project",
		RuntimeDir:      "/tmp/rt",
		TempDir:         "/tmp",
		Network:         NetworkOutbound,
		DenyPorts:       []int{22, 25},
		AllowSubprocess: true,
	}

	runtimeDir := t.TempDir()
	err := s.applyLandlock(cmd, policy, runtimeDir)
	if err != nil {
		t.Fatalf("applyLandlock error: %v", err)
	}

	policyPath := runtimeDir + "/landlock-policy.json"
	policyBytes, err := os.ReadFile(policyPath)
	if err != nil {
		t.Fatalf("reading policy file: %v", err)
	}

	policyJSON := string(policyBytes)
	if !strings.Contains(policyJSON, `"DenyPorts":[22,25]`) {
		t.Errorf("policy JSON should contain DenyPorts=[22,25], got: %s", policyJSON)
	}
}

func TestBwrap_PortFiltering_Warning(t *testing.T) {
	s := &LinuxSandbox{}
	cmd := exec.Command("/usr/bin/echo", "hello")
	policy := Policy{
		ProjectRoot:     "/tmp/project",
		RuntimeDir:      "/tmp/rt",
		TempDir:         "/tmp",
		Network:         NetworkOutbound,
		AllowPorts:      []int{443},
		AllowSubprocess: true,
	}

	bwrapPath, err := exec.LookPath("bwrap")
	if err != nil {
		t.Skip("bwrap not on PATH")
	}

	// Capture log output
	var logBuf bytes.Buffer
	log.SetOutput(&logBuf)
	defer log.SetOutput(os.Stderr)

	err = s.applyBwrap(cmd, policy, bwrapPath)
	if err != nil {
		t.Fatalf("applyBwrap error: %v", err)
	}

	logOutput := logBuf.String()
	if !strings.Contains(logOutput, "Port-level filtering not supported by bwrap") {
		t.Errorf("expected warning about port filtering, got log output: %q", logOutput)
	}

	// Verify execution still proceeds (cmd.Path should be bwrap)
	if cmd.Path != bwrapPath {
		t.Errorf("cmd.Path = %q, want %q (execution should proceed despite warning)", cmd.Path, bwrapPath)
	}
}

func TestFilterEnv(t *testing.T) {
	env := []string{
		"PATH=/usr/bin",
		"HOME=/home/user",
		"SECRET_API_KEY=sk-123",
		"TERM=xterm-256color",
		"ANTHROPIC_API_KEY=sk-ant-123",
		"XDG_CONFIG_HOME=/home/user/.config",
	}

	filtered := filterEnv(env)

	kept := make(map[string]bool)
	for _, e := range filtered {
		key := strings.SplitN(e, "=", 2)[0]
		kept[key] = true
	}

	if !kept["PATH"] {
		t.Error("PATH should be kept")
	}
	if !kept["HOME"] {
		t.Error("HOME should be kept")
	}
	if !kept["TERM"] {
		t.Error("TERM should be kept")
	}
	if !kept["XDG_CONFIG_HOME"] {
		t.Error("XDG_CONFIG_HOME should be kept")
	}
	if kept["SECRET_API_KEY"] {
		t.Error("SECRET_API_KEY should be filtered out")
	}
	if kept["ANTHROPIC_API_KEY"] {
		t.Error("ANTHROPIC_API_KEY should be filtered out")
	}
}
