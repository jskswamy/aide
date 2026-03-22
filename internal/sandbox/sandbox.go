package sandbox

import (
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/jskswamy/aide/pkg/seatbelt"
	"github.com/jskswamy/aide/pkg/seatbelt/modules"
)

// Sandbox applies a security policy to a command before execution.
// OS-specific implementations live in darwin.go and linux.go.
type Sandbox interface {
	// Apply modifies cmd in-place so that when cmd.Run() is called the
	// process executes inside the sandbox. It may:
	//   - Rewrite cmd.Path and cmd.Args (e.g. prefix with sandbox-exec or bwrap)
	//   - Write temporary policy files to runtimeDir
	//   - Modify cmd.Env (for clean_env support)
	//
	// runtimeDir is the ephemeral $XDG_RUNTIME_DIR/aide-<pid>/ directory
	// that is cleaned on exit. Policy files should be written here.
	//
	// Returns an error if the policy cannot be enforced on this OS/kernel.
	Apply(cmd *exec.Cmd, policy Policy, runtimeDir string) error

	// GenerateProfile returns the platform-specific sandbox profile that
	// would be applied for the given policy. On macOS this is the Seatbelt
	// .sb profile, on Linux a description of Landlock/bwrap rules.
	// This is used by "aide sandbox test" for debugging sandbox configuration.
	GenerateProfile(policy Policy) (string, error)
}

// Policy describes the security boundary for an agent process.
type Policy struct {
	// Guards lists the active guard names (resolved from registry).
	Guards []string

	// AgentModule is the agent-specific seatbelt module (e.g. ClaudeAgent).
	AgentModule seatbelt.Module

	// ProjectRoot is the git root (or cwd if not a repo).
	ProjectRoot string

	// RuntimeDir is the ephemeral $XDG_RUNTIME_DIR/aide-<pid>/ directory.
	RuntimeDir string

	// TempDir is the os.TempDir() result.
	TempDir string

	// Env is the environment variables passed to the agent.
	Env []string

	// Network mode: "outbound", "none", "unrestricted".
	Network NetworkMode

	// AllowPorts restricts outbound connections to these ports only (whitelist).
	AllowPorts []int

	// DenyPorts blocks outbound connections to these ports (blacklist).
	DenyPorts []int

	// ExtraDenied holds user-configured denied paths from config.
	ExtraDenied []string

	// Whether the agent may spawn child processes.
	AllowSubprocess bool

	// When true the agent starts with only aide-injected env vars
	// (DD-17). When false the agent inherits the full shell env.
	CleanEnv bool
}

// NetworkMode describes the network access policy for a sandboxed agent.
type NetworkMode string

const (
	// NetworkOutbound allows outbound network connections only.
	NetworkOutbound NetworkMode = "outbound"
	// NetworkNone blocks all network access.
	NetworkNone NetworkMode = "none"
	// NetworkUnrestricted allows all network access (inbound and outbound).
	NetworkUnrestricted NetworkMode = "unrestricted"
)

// DefaultPolicy returns the sandbox policy applied when no sandbox: block
// exists in the context config.
//
// Parameters:
//
//	projectRoot — git root (or cwd if not a repo)
//	runtimeDir  — $XDG_RUNTIME_DIR/aide-<pid>/
//	tempDir     — os.TempDir() result
//	env         — environment variables for the agent
func DefaultPolicy(projectRoot, runtimeDir, tempDir string, env []string) Policy {
	return Policy{
		Guards:          modules.DefaultGuardNames(),
		ProjectRoot:     projectRoot,
		RuntimeDir:      runtimeDir,
		TempDir:         tempDir,
		Env:             env,
		Network:         NetworkOutbound,
		AllowSubprocess: true,
		CleanEnv:        false,
	}
}

// NewSandbox returns a Sandbox implementation for the current platform.
// On macOS it returns darwinSandbox, on unsupported platforms it returns
// a no-op sandbox. Platform-specific implementations are in build-tagged files.
// This function is defined in darwin.go and sandbox_other.go.

// noopSandbox is a fallback Sandbox that does nothing.
// Used when no platform-specific sandbox is available.
type noopSandbox struct{}

// Apply is a no-op; the command runs unsandboxed.
func (n *noopSandbox) Apply(_ *exec.Cmd, _ Policy, _ string) error {
	return nil
}

// GenerateProfile returns a message indicating sandbox is unavailable.
func (n *noopSandbox) GenerateProfile(_ Policy) (string, error) {
	return "Sandbox not available on this platform (no-op sandbox)", nil
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
		"TMPDIR": true, "XDG_RUNTIME_DIR": true, "XDG_CONFIG_HOME": true,
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
