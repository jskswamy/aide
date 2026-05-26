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

	// HomeDir is the user's home directory, resolved once by the caller and
	// stored here so sandbox internals never call os.UserHomeDir() independently.
	// Every ToSeatbeltContext call and guard evaluation reads from this field.
	HomeDir string

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
		HomeDir:         p.HomeDir,
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
func (p *Policy) ToSeatbeltContext() *seatbelt.Context {
	return &seatbelt.Context{
		HomeDir:         p.HomeDir,
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
//   - Guard Writable paths (incl. the agent module's Linux grants routed
//     through EvaluateGuards) → Writable.
//   - policy.ExtraReadable → Readable.
//   - Guard Readable paths → Readable.
//   - All paths are resolved via filepath.EvalSymlinks before use.
func DeriveGrantedPathSet(policy Policy) GrantedPathSet {
	origin := make(map[string]string)

	// Collect guard-protected (denied) paths. Use resolveSymlinkForDeny so
	// that resolution errors never silently drop a protected path — dropping
	// a deny entry could allow a later writable grant to bypass deny-wins.
	deniedSet := make(map[string]bool)
	guardResults := EvaluateGuards(&policy)
	for _, gr := range guardResults {
		for _, p := range gr.Protected {
			resolved := resolveSymlinkForDeny(p)
			deniedSet[resolved] = true
			if origin[resolved] == "" {
				origin[resolved] = gr.Name
			}
		}
	}

	// Add explicit extra-denied paths from policy.
	for _, p := range policy.ExtraDenied {
		for _, expanded := range expandGlobs([]string{p}) {
			resolved := resolveSymlinkForDeny(expanded)
			deniedSet[resolved] = true
			if origin[resolved] == "" {
				origin[resolved] = "config:extra_denied"
			}
		}
	}

	// Collect readable paths from guard Allowed lists (e.g. a capability
	// unlocking ~/.config/gh, or a credential path the agent may read).
	readableExtra := make(map[string]bool)
	for _, gr := range guardResults {
		for _, p := range gr.Allowed {
			resolved := resolveSymlink(p)
			if resolved != "" {
				readableExtra[resolved] = true
				if origin[resolved] == "" {
					origin[resolved] = gr.Name + ":readable"
				}
			}
		}
	}

	// Collect writable paths from guard Writable lists. The agent module's
	// Linux path grants flow through EvaluateGuards as a synthetic guard
	// result, so they land here via the same pipeline as any other
	// path-vouching evaluator — origin tracking, deny-wins, and conflict
	// detection all apply uniformly.
	writableExtra := make(map[string]bool)
	for _, gr := range guardResults {
		for _, p := range gr.Writable {
			resolved := resolveSymlink(p)
			if resolved != "" {
				writableExtra[resolved] = true
				if origin[resolved] == "" {
					origin[resolved] = gr.Name + ":writable"
				}
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
	for p := range writableExtra {
		writableSet[p] = true
	}

	// Aide's own state dirs (~/.config/aide, ~/.local/share/aide,
	// ~/.cache/aide) are intentionally NOT added to the readable set.
	// Granting subtree read on ~/.config/aide would also expose
	// ~/.config/aide/secrets/*.enc.yaml — Landlock has no deny rules and
	// the exact-match deny-wins below cannot withdraw access from inside
	// a parent allow. Agent modules that need specific subdirs (e.g.
	// Claude's ~/.config/aide/claude redirect) declare them through
	// GuardResult.Writable / Readable so each grant is auditable in
	// OriginGuard and bounded to the dir the agent actually needs.
	readableSet := make(map[string]bool)
	for _, p := range policy.ExtraReadable {
		if resolved := resolveSymlink(p); resolved != "" {
			readableSet[resolved] = true
			if origin[resolved] == "" {
				origin[resolved] = "config:extra_readable"
			}
		}
	}
	for p := range readableExtra {
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
//
// For denied (Protected) paths, callers should use resolveSymlinkForDeny which
// always returns a usable path to avoid silently dropping security constraints.
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

// resolveSymlinkForDeny resolves a path for inclusion in the denied (Protected)
// set. Unlike resolveSymlink, it never drops the path on error: returning ""
// would silently remove a security constraint, potentially allowing a
// subsequently-added writable rule to grant access to a protected location.
// On any error the cleaned input path is used as-is.
func resolveSymlinkForDeny(p string) string {
	resolved, err := filepath.EvalSymlinks(p)
	if err != nil {
		return filepath.Clean(p)
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
	activeGuards := guards.ResolveActiveGuards(policy.Guards)

	ctx := policy.ToSeatbeltContext()

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

// filterEnv reduces env to the essential vars an agent needs to function
// in a CleanEnv sandbox. The base allow-list covers shell + locale + XDG
// runtime essentials; on top of that, any env key declared by the policy's
// AgentModule via the seatbelt.EnvProvider interface is preserved too.
//
// Without the EnvProvider passthrough, applyAgentEnv in the launcher would
// inject e.g. CLAUDE_CONFIG_DIR pointing at aide's redirect path, and then
// CleanEnv would immediately strip it back out — Claude would fall back to
// $HOME/.claude*, which Landlock denies, and the agent would crash.
//
// The probe context has Env cleared so EnvProvider implementations that
// "respect user overrides by returning nil" still surface their managed
// key set. We only use the returned map's KEYS — the values in env have
// already been chosen by applyAgentEnv (user value or module default).
func filterEnv(env []string, policy Policy) []string {
	essential := map[string]bool{
		"PATH": true, "HOME": true, "USER": true,
		"SHELL": true, "TERM": true, "LANG": true,
		"TMPDIR": true, "XDG_RUNTIME_DIR": true, "XDG_CONFIG_HOME": true,
	}
	for _, k := range agentEnvKeys(policy) {
		essential[k] = true
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

// agentEnvKeys returns the env keys the policy's AgentModule wants present
// in the agent process. Returns nil when the module does not implement
// seatbelt.EnvProvider (e.g. agents with no env-redirect needs).
//
// Key discovery prefers seatbelt.EnvKeyProvider (side-effect-free) over
// probing AgentEnv with a nil-env context. Probing AgentEnv bypasses the
// "user already set this key" guard and may trigger filesystem side effects
// (e.g. os.MkdirAll in claude_linux.go). If that fails, AgentEnv returns nil,
// making the key invisible to filterEnv — a user-set CLAUDE_CONFIG_DIR would
// then be silently stripped in CleanEnv mode.
func agentEnvKeys(policy Policy) []string {
	if policy.AgentModule == nil {
		return nil
	}
	if _, ok := policy.AgentModule.(seatbelt.EnvProvider); !ok {
		return nil
	}

	ctx := policy.ToSeatbeltContext()

	// Prefer the side-effect-free EnvKeyProvider when available.
	if keyProvider, ok := policy.AgentModule.(seatbelt.EnvKeyProvider); ok {
		keys := keyProvider.AgentEnvKeys(ctx)
		sort.Strings(keys)
		return keys
	}

	// Fallback: probe AgentEnv with Env=nil to force the module to reveal its
	// keys regardless of any user-set override. This path exists for modules
	// that have not yet implemented EnvKeyProvider; it may have side effects.
	provider, _ := policy.AgentModule.(seatbelt.EnvProvider)
	probe := policy.ToSeatbeltContext()
	probe.Env = nil
	envMap := provider.AgentEnv(probe)
	if len(envMap) == 0 {
		return nil
	}
	keys := make([]string, 0, len(envMap))
	for k := range envMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
