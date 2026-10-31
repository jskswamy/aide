//go:build linux && integration

package sandbox

import (
	"fmt"
	"os"
	"os/exec"
	"testing"
)

func init() {
	if len(os.Args) > 2 && os.Args[1] == "__pi-managed-landlock" {
		if err := RunSandboxApply("3", os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	if len(os.Args) > 4 && os.Args[1] == "__sandbox-apply" && os.Args[2] == "--" {
		if err := RunSandboxExec(os.Args[3], os.Args[4:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
}

func piManagedBackends() []string { return []string{BackendLandlock, BackendBwrap} }
func piManagedApply(t *testing.T, cmd *exec.Cmd, policy Policy, backend string) {
	t.Helper()
	s := &LinuxSandbox{}
	switch backend {
	case BackendLandlock:
		caps := DetectKernelCapabilities()
		if !caps.LandlockEnabled || caps.LandlockABI < 4 {
			t.Skip("managed Pi integration requires Landlock ABI >=4")
		}
		if err := s.applyLandlock(cmd, policy, policy.RuntimeDir); err != nil {
			t.Fatal(err)
		}
		// exec.Cmd remaps ExtraFiles to fd 3; the normal launcher instead uses raw exec.
		args := append([]string{cmd.Path, "__pi-managed-landlock"}, cmd.Args[4:]...)
		cmd.Args = args
	case BackendBwrap:
		skipIfNoBwrapMount(t)
		binary, err := exec.LookPath("bwrap")
		if err != nil {
			t.Skip("bubblewrap unavailable")
		}
		if err := s.applyBwrap(cmd, policy, binary); err != nil {
			t.Fatal(err)
		}
		if !policy.AllowSubprocess {
			// The seccomp memfd is also remapped by exec.Cmd rather than raw exec.
			for i, arg := range cmd.Args {
				if arg == "--seccomp" {
					cmd.Args[i+1] = "3"
				}
			}
		}
	}
}
