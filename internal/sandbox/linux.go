//go:build linux

package sandbox

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/landlock-lsm/go-landlock/landlock"
)

// LinuxSandbox implements Sandbox using Landlock (preferred) or bubblewrap (fallback).
type LinuxSandbox struct{}

// NewSandbox returns a Linux-specific sandbox implementation.
func NewSandbox() Sandbox {
	return &LinuxSandbox{}
}

// Apply applies the sandbox policy to the command.
// Tries Landlock first (kernel 5.13+), falls back to bwrap, or proceeds unsandboxed.
func (l *LinuxSandbox) Apply(cmd *exec.Cmd, policy Policy, runtimeDir string) error {
	if landlockAvailable() {
		return l.applyLandlock(cmd, policy, runtimeDir)
	}
	if bwrapPath, err := exec.LookPath("bwrap"); err == nil {
		return l.applyBwrap(cmd, policy, bwrapPath)
	}
	// Neither available — log warning, proceed unsandboxed
	log.Println("warning: sandboxing unavailable: kernel lacks Landlock and bwrap not on PATH")
	return nil
}

// landlockAvailable checks if the kernel supports Landlock by reading
// the LSM list from /sys/kernel/security/lsm.
func landlockAvailable() bool {
	data, err := os.ReadFile("/sys/kernel/security/lsm")
	if err != nil {
		return false
	}
	for _, lsm := range strings.Split(strings.TrimSpace(string(data)), ",") {
		if lsm == "landlock" {
			return true
		}
	}
	return false
}

// applyLandlock uses the re-exec pattern to apply Landlock in a child process.
// aide re-execs itself with __sandbox-apply which applies Landlock restrictions
// then execs the actual agent. This is necessary because Landlock is self-sandboxing
// (restricts the calling process).
func (l *LinuxSandbox) applyLandlock(cmd *exec.Cmd, policy Policy, runtimeDir string) error {
	policyBytes, err := json.Marshal(policy)
	if err != nil {
		return fmt.Errorf("marshal sandbox policy: %w", err)
	}

	policyPath := filepath.Join(runtimeDir, "landlock-policy.json")
	if err := os.WriteFile(policyPath, policyBytes, 0600); err != nil {
		return fmt.Errorf("write sandbox policy: %w", err)
	}

	aideBin, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve aide binary: %w", err)
	}

	originalArgs := cmd.Args
	cmd.Path = aideBin
	cmd.Args = append(
		[]string{"aide", "__sandbox-apply", policyPath, "--"},
		originalArgs...,
	)

	if policy.CleanEnv {
		cmd.Env = filterEnv(cmd.Env)
	}

	return nil
}

// RunSandboxApply is the handler for the __sandbox-apply hidden subcommand.
// It reads the policy, applies Landlock restrictions, then execs the agent.
// This runs in a child process, so Landlock restricts only this process + the agent.
func RunSandboxApply(policyPath string, agentCmd []string) error {
	policyBytes, err := os.ReadFile(policyPath)
	if err != nil {
		return fmt.Errorf("read sandbox policy: %w", err)
	}

	var policy Policy
	if err := json.Unmarshal(policyBytes, &policy); err != nil {
		return fmt.Errorf("unmarshal sandbox policy: %w", err)
	}

	// Build Landlock rules using high-level helpers.
	// Landlock is default-deny: only explicitly allowed paths are accessible.
	// Denied paths are implicitly blocked by not appearing in allow lists.
	var rules []landlock.Rule

	for _, p := range policy.Writable {
		rules = append(rules, landlock.RWDirs(p))
	}

	for _, p := range policy.Readable {
		rules = append(rules, landlock.RODirs(p))
	}

	// Apply with BestEffort for graceful degradation on older kernels.
	// V5 covers FS + network + ioctl; BestEffort downgrades to whatever
	// the kernel actually supports.
	if err := landlock.V5.BestEffort().Restrict(rules...); err != nil {
		return fmt.Errorf("landlock restrict: %w", err)
	}

	// Exec the agent, replacing this process
	agentPath, err := exec.LookPath(agentCmd[0])
	if err != nil {
		return fmt.Errorf("agent not found: %w", err)
	}

	return syscall.Exec(agentPath, agentCmd, os.Environ())
}

// applyBwrap wraps the command with bubblewrap for filesystem isolation.
func (l *LinuxSandbox) applyBwrap(cmd *exec.Cmd, policy Policy, bwrapPath string) error {
	var bwrapArgs []string

	// Writable paths: --bind src src
	for _, p := range policy.Writable {
		bwrapArgs = append(bwrapArgs, "--bind", p, p)
	}

	// Readable paths: --ro-bind src src
	for _, p := range policy.Readable {
		bwrapArgs = append(bwrapArgs, "--ro-bind", p, p)
	}

	// System essentials
	bwrapArgs = append(bwrapArgs,
		"--ro-bind", "/usr", "/usr",
		"--ro-bind", "/etc/resolv.conf", "/etc/resolv.conf",
		"--proc", "/proc",
		"--dev", "/dev",
		"--tmpfs", "/tmp",
	)

	// Add /lib and /lib64 if they exist
	for _, lib := range []string{"/lib", "/lib64"} {
		if _, err := os.Stat(lib); err == nil {
			bwrapArgs = append(bwrapArgs, "--ro-bind", lib, lib)
		}
	}

	// Denied paths: mask with empty tmpfs
	for _, p := range expandGlobs(policy.Denied) {
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			bwrapArgs = append(bwrapArgs, "--tmpfs", p)
		}
		// For files, the parent dir restriction handles it
	}

	// Network isolation
	if policy.Network == NetworkNone {
		bwrapArgs = append(bwrapArgs, "--unshare-net")
	}

	// Subprocess control
	if !policy.AllowSubprocess {
		// bwrap doesn't directly limit subprocess creation,
		// but we can unshare PID namespace for isolation
		bwrapArgs = append(bwrapArgs, "--unshare-pid")
	}

	// Append -- and the original command
	bwrapArgs = append(bwrapArgs, "--")
	bwrapArgs = append(bwrapArgs, cmd.Args...)

	// Rewrite the command
	cmd.Path = bwrapPath
	cmd.Args = append([]string{"bwrap"}, bwrapArgs...)

	if policy.CleanEnv {
		cmd.Env = filterEnv(cmd.Env)
	}

	return nil
}

// filterEnv and expandGlobs are in sandbox.go (shared across platforms).
