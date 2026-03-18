//go:build darwin

package sandbox

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

// generateSeatbeltProfile builds a Seatbelt .sb profile string from a Policy.
func generateSeatbeltProfile(policy Policy) (string, error) {
	var b strings.Builder

	// Header
	b.WriteString("(version 1)\n")
	b.WriteString("(deny default)\n")

	// Process rules
	b.WriteString("\n;; --- Process ---\n")
	b.WriteString("(allow process-exec)\n")
	if policy.AllowSubprocess {
		b.WriteString("(allow process-fork)\n")
	}

	// Network rules
	switch policy.Network {
	case NetworkOutbound:
		b.WriteString("\n;; --- Network ---\n")
		b.WriteString("(allow network-outbound)\n")
	case NetworkUnrestricted:
		b.WriteString("\n;; --- Network ---\n")
		b.WriteString("(allow network*)\n")
	case NetworkNone:
		// deny default covers it
	}

	// Filesystem: denied paths (evaluated first by sandbox-exec for precedence)
	deniedPaths := expandGlobs(policy.Denied)
	if len(deniedPaths) > 0 {
		b.WriteString("\n;; --- Filesystem: denied ---\n")
		for _, p := range deniedPaths {
			expr := seatbeltPath(p)
			b.WriteString(fmt.Sprintf("(deny file-read* %s)\n", expr))
			b.WriteString(fmt.Sprintf("(deny file-write* %s)\n", expr))
		}
	}

	// Filesystem: writable paths
	if len(policy.Writable) > 0 {
		b.WriteString("\n;; --- Filesystem: writable ---\n")
		for _, p := range policy.Writable {
			expr := seatbeltPath(p)
			b.WriteString(fmt.Sprintf("(allow file-read* %s)\n", expr))
			b.WriteString(fmt.Sprintf("(allow file-write* %s)\n", expr))
		}
	}

	// Filesystem: readable paths
	if len(policy.Readable) > 0 {
		b.WriteString("\n;; --- Filesystem: readable ---\n")
		for _, p := range policy.Readable {
			expr := seatbeltPath(p)
			b.WriteString(fmt.Sprintf("(allow file-read* %s)\n", expr))
		}
	}

	// System essentials (always allowed)
	b.WriteString("\n;; --- System essentials ---\n")
	b.WriteString("(allow file-read*\n")
	b.WriteString("    (subpath \"/usr/lib\")\n")
	b.WriteString("    (subpath \"/System/Library\")\n")
	b.WriteString("    (subpath \"/Library/Frameworks\")\n")
	b.WriteString("    (subpath \"/private/var/db/dyld\")\n")
	b.WriteString("    (literal \"/dev/null\")\n")
	b.WriteString("    (literal \"/dev/urandom\")\n")
	b.WriteString("    (literal \"/dev/random\")\n")
	b.WriteString(")\n")
	b.WriteString("(allow sysctl-read)\n")
	b.WriteString("(allow mach-lookup)\n")

	return b.String(), nil
}

// seatbeltPath returns the Seatbelt path expression for a filesystem path.
// Directories use (subpath ...), files use (literal ...).
func seatbeltPath(p string) string {
	info, err := os.Stat(p)
	if err == nil && info.IsDir() {
		return fmt.Sprintf(`(subpath "%s")`, p)
	}
	return fmt.Sprintf(`(literal "%s")`, p)
}

// expandGlobs expands glob patterns in a list of paths.
// Non-glob paths are passed through unchanged.
func expandGlobs(patterns []string) []string {
	var result []string
	for _, p := range patterns {
		if strings.ContainsAny(p, "*?[") {
			matches, _ := filepath.Glob(p)
			result = append(result, matches...)
		} else {
			result = append(result, p)
		}
	}
	return result
}

// filterEnv returns only essential env vars when CleanEnv is true (DD-17).
func filterEnv(env []string) []string {
	essential := map[string]bool{
		"PATH": true, "HOME": true, "USER": true,
		"SHELL": true, "TERM": true, "LANG": true,
		"TMPDIR": true, "XDG_RUNTIME_DIR": true,
	}
	var filtered []string
	for _, e := range env {
		k := strings.SplitN(e, "=", 2)[0]
		if essential[k] {
			filtered = append(filtered, e)
		}
	}
	return filtered
}
