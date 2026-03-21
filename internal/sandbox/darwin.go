//go:build darwin

// Package sandbox provides OS-native sandboxing for agent processes.
package sandbox

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/jskswamy/aide/pkg/seatbelt"
	"github.com/jskswamy/aide/pkg/seatbelt/modules"
)

// NewSandbox returns a darwinSandbox on macOS.
func NewSandbox() Sandbox {
	return &darwinSandbox{}
}

// darwinSandbox implements the Sandbox interface for macOS using Apple's
// Seatbelt framework via sandbox-exec.
type darwinSandbox struct{}

// Apply generates a Seatbelt .sb profile from the policy, writes it to
// runtimeDir, and rewrites cmd to invoke the original command through
// sandbox-exec -f <profile-path>.
func (d *darwinSandbox) Apply(cmd *exec.Cmd, policy Policy, runtimeDir string) error {
	// 1. Generate Seatbelt profile string from policy
	profile, err := generateSeatbeltProfile(policy)
	if err != nil {
		return fmt.Errorf("generating seatbelt profile: %w", err)
	}

	// 2. Write profile to runtimeDir
	profilePath := filepath.Join(runtimeDir, "sandbox.sb")
	if err := os.WriteFile(profilePath, []byte(profile), 0600); err != nil {
		return fmt.Errorf("writing seatbelt profile: %w", err)
	}

	// 3. Rewrite cmd to wrap with sandbox-exec
	originalArgs := cmd.Args // Args[0] is the program name

	cmd.Path = "/usr/bin/sandbox-exec"
	cmd.Args = append(
		[]string{"sandbox-exec", "-f", profilePath},
		originalArgs...,
	)

	// 4. Handle clean_env (DD-17)
	if policy.CleanEnv {
		cmd.Env = filterEnv(cmd.Env)
	}

	return nil
}

// GenerateProfile returns the Seatbelt .sb profile string for the given policy.
func (d *darwinSandbox) GenerateProfile(policy Policy) (string, error) {
	return generateSeatbeltProfile(policy)
}

// generateSeatbeltProfile builds a Seatbelt .sb profile string from a Policy
// by composing modules from the pkg/seatbelt library.
func generateSeatbeltProfile(policy Policy) (string, error) {
	homeDir, _ := os.UserHomeDir()

	p := seatbelt.New(homeDir).
		Use(
			modules.Base(),
			modules.SystemRuntime(),
			networkModule(policy),
			modules.Filesystem(modules.FilesystemConfig{
				Writable: policy.Writable,
				Readable: policy.Readable,
				Denied:   policy.Denied,
			}),
			modules.NodeToolchain(),
			modules.NixToolchain(),
			modules.GitIntegration(),
			modules.KeychainIntegration(),
			modules.ClaudeAgent(),
		)

	return p.Render()
}

// networkModule maps the sandbox package's NetworkMode to a seatbelt network module.
func networkModule(policy Policy) seatbelt.Module {
	switch policy.Network {
	case NetworkNone:
		return modules.Network(modules.NetworkNone)
	case NetworkOutbound:
		if len(policy.AllowPorts) > 0 || len(policy.DenyPorts) > 0 {
			return modules.NetworkWithPorts(modules.NetworkOutbound, modules.PortOpts{
				AllowPorts: policy.AllowPorts,
				DenyPorts:  policy.DenyPorts,
			})
		}
		return modules.Network(modules.NetworkOutbound)
	default:
		return modules.Network(modules.NetworkOpen)
	}
}
