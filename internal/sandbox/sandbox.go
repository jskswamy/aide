package sandbox

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"github.com/jskswamy/aide/pkg/seatbelt"
	"github.com/jskswamy/aide/pkg/seatbelt/guards"
)

//go:generate mockgen -destination=mocks/mock_sandbox.go -package=mocks github.com/jskswamy/aide/internal/sandbox Sandbox

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

	// SSHPorts is the list of ports allowed by the ssh guard (from
	// capabilities.ssh.ports), unioned with the guard's auto-detected set.
	SSHPorts []int

	// ExtraDenied holds user-configured denied paths from config.
	ExtraDenied []string

	// ExtraWritable holds user-configured extra writable paths from config.
	ExtraWritable []string

	// ExtraReadable holds user-configured extra readable paths from config.
	ExtraReadable []string

	// ExtraAllow holds non-filesystem Seatbelt operations to allow
	// (e.g. "mach-lookup", "iokit-open", "signal").
	ExtraAllow []string

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

// Paths bundles the filesystem locations a sandbox policy needs. Passing
// the four paths as a struct (instead of a positional string list) makes
// it impossible to swap HomeDir and TempDir at a call site.
type Paths struct {
	// ProjectRoot is the git root (or cwd if not a repo).
	ProjectRoot string
	// RuntimeDir is the $XDG_RUNTIME_DIR/aide-<pid>/ directory.
	RuntimeDir string
	// HomeDir is the user's home directory. Used by PolicyFromConfig for
	// path-template expansion; DefaultPolicy ignores it.
	HomeDir string
	// TempDir is the os.TempDir() result.
	TempDir string
}

// DefaultPolicy returns the sandbox policy applied when no sandbox: block
// exists in the context config.
func DefaultPolicy(p Paths, env []string) Policy {
	return Policy{
		Guards:          guards.DefaultGuardNames(),
		ProjectRoot:     p.ProjectRoot,
		RuntimeDir:      p.RuntimeDir,
		TempDir:         p.TempDir,
		Env:             env,
		Network:         NetworkOutbound,
		AllowSubprocess: true,
		CleanEnv:        false,
	}
}

// NewSandbox is provided per platform in darwin.go, linux.go, sandbox_other.go.

type noopSandbox struct{}

func (n *noopSandbox) Apply(_ *exec.Cmd, _ Policy, _ string) error {
	return nil
}

func (n *noopSandbox) GenerateProfile(_ Policy) (string, error) {
	return "Sandbox not available on this platform (no-op sandbox)", nil
}

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

// ToSeatbeltContext projects the Policy onto a seatbelt.Context. It is the
// single owner of the Policy → Context mapping; both EvaluateGuards and the
// darwin profile generator route through it so that adding a Policy field
// is a one-line change in this method instead of a shotgun edit across
// sandbox.go and darwin.go.
func (p *Policy) ToSeatbeltContext(homeDir string) *seatbelt.Context {
	return &seatbelt.Context{
		HomeDir:         homeDir,
		ProjectRoot:     p.ProjectRoot,
		TempDir:         p.TempDir,
		RuntimeDir:      p.RuntimeDir,
		Env:             p.Env,
		GOOS:            runtime.GOOS,
		Network:         string(p.Network),
		AllowPorts:      p.AllowPorts,
		DenyPorts:       p.DenyPorts,
		SSHPorts:        p.SSHPorts,
		ExtraDenied:     p.ExtraDenied,
		ExtraWritable:   p.ExtraWritable,
		ExtraReadable:   p.ExtraReadable,
		AllowSubprocess: p.AllowSubprocess,
		ExtraAllow:      p.ExtraAllow,
	}
}

// GrantedPathSet is the projection of a sandbox policy onto concrete filesystem paths.
// The deny-wins invariant is enforced: paths in Denied are always absent from
// Writable and Readable before paths reach the backend enforcer.
type GrantedPathSet struct {
	// Writable is the set of absolute paths the agent may read and write.
	Writable []string

	// Readable is the set of absolute paths the agent may read (but not write).
	Readable []string

	// Denied is the set of absolute paths that are explicitly inaccessible,
	// regardless of any allow rule in Writable or Readable.
	Denied []string

	// OriginGuard maps each path to the guard name that produced the rule.
	// Used by aide sandbox show for annotated output.
	OriginGuard map[string]string
}

// DeriveGrantedPathSet computes the concrete path set for Linux enforcement
// by evaluating active guards and merging explicit policy fields.
//
// Design:
//   - Guard Protected paths → Denied (deny wins over any allow).
//   - Explicit policy.ExtraDenied → also Denied.
//   - policy.ProjectRoot, RuntimeDir, TempDir, ExtraWritable → Writable.
//   - policy.ExtraReadable → Readable.
//   - Guard Allowed paths (explicit exceptions) → not added to Denied.
//   - All paths are resolved via filepath.EvalSymlinks before use.
func DeriveGrantedPathSet(policy Policy) GrantedPathSet {
	homeDir, _ := os.UserHomeDir()
	origin := make(map[string]string)

	// Collect guard-protected (denied) paths.
	deniedSet := make(map[string]bool)
	guardResults := EvaluateGuards(&policy)
	for _, gr := range guardResults {
		for _, p := range gr.Protected {
			resolved := resolveSymlink(p)
			if resolved == "" {
				continue
			}
			deniedSet[resolved] = true
			if origin[resolved] == "" {
				origin[resolved] = gr.Name
			}
		}
	}

	// Add explicit extra-denied paths from policy.
	for _, p := range policy.ExtraDenied {
		for _, expanded := range expandGlobs([]string{p}) {
			resolved := resolveSymlink(expanded)
			if resolved == "" {
				continue
			}
			deniedSet[resolved] = true
			if origin[resolved] == "" {
				origin[resolved] = "config:extra_denied"
			}
		}
	}

	// Collect system-required readable paths from guard Allowed lists.
	// Guard Allowed entries are explicit exceptions (e.g. a capability unlocking ~/.config/gh).
	allowedExtra := make(map[string]bool)
	for _, gr := range guardResults {
		for _, p := range gr.Allowed {
			resolved := resolveSymlink(p)
			if resolved != "" {
				allowedExtra[resolved] = true
				if origin[resolved] == "" {
					origin[resolved] = gr.Name + ":allowed"
				}
			}
		}
	}

	// Linux-only paths from the active AgentModule. agentLinuxPaths is a stub
	// on non-Linux builds (see agent_paths_{linux,other}.go); DD-28 keeps this
	// strictly AgentModule-sourced — guards never bypass deny-wins via this.
	ctx := policy.ToSeatbeltContext(homeDir)
	linuxReadableExtra := make(map[string]bool)
	linuxWritableExtra := make(map[string]bool)
	readable, writable := agentLinuxPaths(policy.AgentModule, ctx)
	for _, p := range readable {
		if resolved := resolveSymlink(p); resolved != "" {
			linuxReadableExtra[resolved] = true
			if origin[resolved] == "" {
				origin[resolved] = "agent:linux-readable"
			}
		}
	}
	for _, p := range writable {
		if resolved := resolveSymlink(p); resolved != "" {
			linuxWritableExtra[resolved] = true
			if origin[resolved] == "" {
				origin[resolved] = "agent:linux-writable"
			}
		}
	}

	// Build writable set: policy runtime paths + user config.
	writableSet := make(map[string]bool)
	for _, p := range []string{policy.ProjectRoot, policy.RuntimeDir, policy.TempDir} {
		if p != "" {
			if resolved := resolveSymlink(p); resolved != "" {
				writableSet[resolved] = true
				if origin[resolved] == "" {
					origin[resolved] = "policy:runtime"
				}
			}
		}
	}
	for _, p := range policy.ExtraWritable {
		if resolved := resolveSymlink(p); resolved != "" {
			writableSet[resolved] = true
			if origin[resolved] == "" {
				origin[resolved] = "config:extra_writable"
			}
		}
	}
	for p := range linuxWritableExtra {
		writableSet[p] = true
	}

	readableSet := make(map[string]bool)
	if homeDir != "" {
		// Scope aide's own state to specific subdirs rather than all of $HOME.
		for _, rel := range []string{".config/aide", ".local/share/aide", ".cache/aide"} {
			p := filepath.Join(homeDir, rel)
			if resolved := resolveSymlink(p); resolved != "" {
				readableSet[resolved] = true
				if origin[resolved] == "" {
					origin[resolved] = "guard:filesystem"
				}
			}
		}
	}
	for _, p := range policy.ExtraReadable {
		if resolved := resolveSymlink(p); resolved != "" {
			readableSet[resolved] = true
			if origin[resolved] == "" {
				origin[resolved] = "config:extra_readable"
			}
		}
	}
	for p := range allowedExtra {
		readableSet[p] = true
	}
	for p := range linuxReadableExtra {
		readableSet[p] = true
	}

	// Apply deny-wins: remove any denied path from writable and readable.
	for p := range deniedSet {
		delete(writableSet, p)
		delete(readableSet, p)
	}

	return GrantedPathSet{
		Writable:    sortedKeys(writableSet),
		Readable:    sortedKeys(readableSet),
		Denied:      sortedKeys(deniedSet),
		OriginGuard: origin,
	}
}

// resolveSymlink wraps filepath.EvalSymlinks. Returns "" on error (path does
// not exist or cannot be resolved) — callers drop such paths rather than
// expanding them unexpectedly.
func resolveSymlink(p string) string {
	resolved, err := filepath.EvalSymlinks(p)
	if err != nil {
		// Path may not exist yet (e.g. runtime dir not created). Return as-is
		// for non-existent paths that are still valid targets.
		if os.IsNotExist(err) {
			return filepath.Clean(p)
		}
		return ""
	}
	return resolved
}

func sortedKeys(m map[string]bool) []string {
	if len(m) == 0 {
		return nil
	}
	result := make([]string, 0, len(m))
	for k := range m {
		result = append(result, k)
	}
	sort.Strings(result)
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

	ctx := policy.ToSeatbeltContext(homeDir)

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
