package sandbox

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/jskswamy/aide/pkg/seatbelt"
	"github.com/jskswamy/aide/pkg/seatbelt/guards"
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

	// ExtraWritable holds user-configured extra writable paths from config.
	ExtraWritable []string

	// ExtraReadable holds user-configured extra readable paths from config.
	ExtraReadable []string

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
		Guards:          guards.DefaultGuardNames(),
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
// Used by linux.go — appears unused on darwin.
func expandGlobs(patterns []string) []string { //nolint:unused,nolintlint // used by linux.go, not compiled on darwin
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
		ExtraDenied:   policy.ExtraDenied,
		ExtraWritable:   policy.ExtraWritable,
		ExtraReadable:   policy.ExtraReadable,
		AllowSubprocess: policy.AllowSubprocess,
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

// DetectGuardConflicts scans guard results for cases where a deny rule
// from one guard covers a path that another guard explicitly allows.
// Returns human-readable warning strings for the banner.
//
// Known limitation: only detects exact path matches, not subpath-covers-literal
// overlaps (e.g., deny subpath "/tmp" vs allow literal "/tmp/foo").
func DetectGuardConflicts(results []seatbelt.GuardResult) []string {
	type pathEntry struct {
		guard string
		path  string
	}

	var denied []pathEntry
	var allowed []pathEntry

	pathRe := regexp.MustCompile(`"([^"]+)"`)

	for _, r := range results {
		for _, rule := range r.Rules {
			text := rule.String()
			matches := pathRe.FindAllStringSubmatch(text, -1)
			for _, m := range matches {
				p := m[1]
				if strings.Contains(text, "deny ") {
					denied = append(denied, pathEntry{guard: r.Name, path: p})
				} else if strings.Contains(text, "allow ") {
					allowed = append(allowed, pathEntry{guard: r.Name, path: p})
				}
			}
		}
	}

	var warnings []string
	for _, d := range denied {
		for _, a := range allowed {
			if d.guard != a.guard && d.path == a.path {
				warnings = append(warnings,
					fmt.Sprintf("guard %q denies %s which guard %q allows (deny wins in seatbelt)",
						d.guard, d.path, a.guard))
			}
		}
	}
	return warnings
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
