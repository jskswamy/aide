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
// by composing modules from the guard registry.
func generateSeatbeltProfile(policy Policy) (string, error) {
	homeDir, _ := os.UserHomeDir()

	// Map sandbox.NetworkMode (string) to seatbelt.NetworkMode (int)
	var netMode seatbelt.NetworkMode
	switch policy.Network {
	case NetworkNone:
		netMode = seatbelt.NetworkNone
	case NetworkOutbound:
		netMode = seatbelt.NetworkOutbound
	default:
		netMode = seatbelt.NetworkOpen
	}

	// Resolve active guards from names
	guardModules := modules.ResolveActiveGuards(policy.Guards)

	// Create profile with context
	p := seatbelt.New(homeDir).
		WithContext(func(ctx *seatbelt.Context) {
			ctx.ProjectRoot = policy.ProjectRoot
			ctx.TempDir = policy.TempDir
			ctx.RuntimeDir = policy.RuntimeDir
			ctx.Env = policy.Env
			ctx.Network = netMode
			ctx.AllowPorts = policy.AllowPorts
			ctx.DenyPorts = policy.DenyPorts
			ctx.ExtraDenied = policy.ExtraDenied
		})

	// Use each guard module
	for _, g := range guardModules {
		p.Use(g)
	}

	// Use agent module if set
	if policy.AgentModule != nil {
		p.Use(policy.AgentModule)
	}

	return p.Render()
}
