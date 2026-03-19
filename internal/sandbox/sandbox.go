package sandbox

import (
	"os/exec"
	"path/filepath"
	"strings"
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
}

// Policy describes the security boundary for an agent process.
type Policy struct {
	// Filesystem paths the agent may write to.
	Writable []string

	// Filesystem paths the agent may read (but not write).
	Readable []string

	// Filesystem paths the agent must not access at all.
	// Denied rules take precedence over Writable/Readable.
	Denied []string

	// Network mode: "outbound", "none", "unrestricted".
	Network NetworkMode

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
// exists in the context config (DD-15).
//
// Parameters:
//
//	projectRoot — git root (or cwd if not a repo)
//	runtimeDir  — $XDG_RUNTIME_DIR/aide-<pid>/
//	homeDir     — user's home directory (~)
//	tempDir     — os.TempDir() result
func DefaultPolicy(projectRoot, runtimeDir, homeDir, tempDir string) Policy {
	return Policy{
		Writable: []string{
			projectRoot,
			runtimeDir,
			tempDir,
		},
		Readable: []string{
			projectRoot,
			"/usr/bin",
			"/usr/local/bin",
			"/bin",
			"/usr/lib",
			"/usr/share",
			filepath.Join(homeDir, ".gitconfig"),
			filepath.Join(homeDir, ".config/git"),
			filepath.Join(homeDir, ".ssh/known_hosts"),
		},
		Denied: []string{
			filepath.Join(homeDir, ".ssh/id_*"),
			filepath.Join(homeDir, ".aws/credentials"),
			filepath.Join(homeDir, ".azure"),
			filepath.Join(homeDir, ".config/gcloud"),
			filepath.Join(homeDir, ".config/aide/secrets"),
			filepath.Join(homeDir, "Library/Application Support/Google/Chrome"),
			filepath.Join(homeDir, ".mozilla"),
			filepath.Join(homeDir, "snap/chromium"),
		},
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
