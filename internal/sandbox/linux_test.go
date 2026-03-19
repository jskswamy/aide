//go:build linux

package sandbox

import (
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
		Writable:        []string{"/tmp/project"},
		Readable:        []string{"/usr", "/etc"},
		Denied:          []string{},
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

	// Check readable ro-bind
	if !strings.Contains(args, "--ro-bind /usr /usr") {
		t.Errorf("missing --ro-bind for readable path in: %s", args)
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
	policy := DefaultPolicy("/tmp/proj", "/tmp/runtime", "/root", "/tmp")

	// This test exercises the full Apply path.
	// On systems with bwrap (our devcontainer), it will use bwrap.
	// On systems with Landlock, it will use Landlock.
	// Either way, it should not error.
	err := s.Apply(cmd, policy, "/tmp/runtime")
	if err != nil {
		t.Fatalf("Apply should not error: %v", err)
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
