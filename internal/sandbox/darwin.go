//go:build darwin

// Package sandbox provides OS-native sandboxing for agent processes.
package sandbox

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/jskswamy/aide/pkg/seatbelt"
	"github.com/jskswamy/aide/pkg/seatbelt/guards"
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

	// Safety: ensure base guard is present
	hasBase := false
	for _, name := range policy.Guards {
		if name == "base" {
			hasBase = true
			break
		}
	}
	if !hasBase {
		return "", fmt.Errorf("guard 'base' is required but not in Guards list")
	}

	activeGuards := guards.ResolveActiveGuards(policy.Guards)

	p := seatbelt.New(homeDir).
		WithContext(func(c *seatbelt.Context) {
			c.ProjectRoot = policy.ProjectRoot
			c.TempDir = policy.TempDir
			c.RuntimeDir = policy.RuntimeDir
			c.Env = policy.Env
			c.GOOS = runtime.GOOS // FIX C1: always set GOOS
			c.Network = string(policy.Network)
			c.AllowPorts = policy.AllowPorts
			c.DenyPorts = policy.DenyPorts
			c.ExtraDenied = policy.ExtraDenied
		})

	for _, g := range activeGuards {
		p.Use(g)
	}

	if policy.AgentModule != nil {
		p.Use(policy.AgentModule)
	}

	result, err := p.Render()
	if err != nil {
		return "", err
	}
	return result.Profile, nil
}

// EvaluateGuards runs all guards from the policy and returns their diagnostics
// without rendering a full profile. Used by the banner layer to show guard status.
func EvaluateGuards(policy *Policy) []seatbelt.GuardResult {
	if policy == nil {
		return nil
	}
	homeDir, _ := os.UserHomeDir()
	activeGuards := guards.ResolveActiveGuards(policy.Guards)

	ctx := &seatbelt.Context{
		HomeDir:     homeDir,
		ProjectRoot: policy.ProjectRoot,
		TempDir:     policy.TempDir,
		RuntimeDir:  policy.RuntimeDir,
		Env:         policy.Env,
		GOOS:        runtime.GOOS,
		Network:     string(policy.Network),
		AllowPorts:  policy.AllowPorts,
		DenyPorts:   policy.DenyPorts,
		ExtraDenied: policy.ExtraDenied,
	}

	var results []seatbelt.GuardResult
	for _, g := range activeGuards {
		result := g.Rules(ctx)
		result.Name = g.Name()
		results = append(results, result)
	}
	if policy.AgentModule != nil {
		result := policy.AgentModule.Rules(ctx)
		result.Name = policy.AgentModule.Name()
		results = append(results, result)
	}
	return results
}

// AvailableGuardNames returns opt-in guard names not included in the active list.
func AvailableGuardNames(activeNames []string) []string {
	active := make(map[string]bool)
	for _, n := range activeNames {
		active[n] = true
	}
	var available []string
	for _, g := range guards.AllGuards() {
		if g.Type() == "opt-in" && !active[g.Name()] {
			available = append(available, g.Name())
		}
	}
	return available
}
