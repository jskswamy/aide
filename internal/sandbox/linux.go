//go:build linux

// Package sandbox implements OS-native sandboxing for agent processes.
package sandbox

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/landlock-lsm/go-landlock/landlock"
)

// LinuxSandbox implements Sandbox using Landlock (preferred) or bubblewrap (fallback).
type LinuxSandbox struct {
	lastTier *IsolationTier
}

// LastTier returns the IsolationTier computed during the most recent Apply call.
func (l *LinuxSandbox) LastTier() *IsolationTier {
	return l.lastTier
}

// NewSandbox returns a Sandbox backed by Landlock (preferred) or bubblewrap.
func NewSandbox() Sandbox {
	return &LinuxSandbox{}
}

// Apply configures cmd to run under the best available OS-level sandbox.
// Landlock is preferred; bubblewrap is used as a fallback when Landlock is
// absent. Returns an error when the policy requires enforcement that the
// available backend cannot honour.
func (l *LinuxSandbox) Apply(cmd *exec.Cmd, policy Policy, runtimeDir string) error {
	caps := DetectKernelCapabilities()
	tier := ComputeIsolationTier(caps, policy)
	l.lastTier = &tier

	if tier.Tier == TierUnavailable {
		fmt.Fprintf(os.Stderr, "aide: warning: OS-level sandboxing unavailable: %s\n", tier.Reason)
		return nil
	}

	if caps.LandlockEnabled {
		if tier.Tier == TierDegraded {
			hasPortRules := len(policy.AllowPorts) > 0 || len(policy.DenyPorts) > 0
			remedy := "upgrade to kernel ≥ 6.7 (Landlock ABI 4) or set network to unrestricted"
			if hasPortRules {
				remedy = "upgrade to kernel ≥ 6.7 (Landlock ABI 4) or remove port rules"
			}
			return fmt.Errorf("sandbox: %s; %s", tier.Reason, remedy)
		}
		return l.applyLandlock(cmd, policy, runtimeDir)
	}
	if bwrapPath, err := exec.LookPath("bwrap"); err == nil {
		if tier.Tier == TierDegraded {
			fmt.Fprintf(os.Stderr, "aide: warning: sandbox degraded: %s\n", tier.Reason)
		}
		return l.applyBwrap(cmd, policy, bwrapPath)
	}
	fmt.Fprintf(os.Stderr, "aide: warning: OS-level sandboxing unavailable: no Landlock and no bwrap\n")
	return nil
}

// linuxSystemReadable is the minimal Landlock allow-list needed for any
// process to exec binaries and load shared libraries. Landlock denies by
// default; bwrap handles these separately via --ro-bind.
var linuxSystemReadable = []string{
	"/usr",
	"/bin",
	"/sbin",
	"/lib",
	"/lib64",
	"/lib32",
	"/libx32",
	"/proc",
	"/sys",  // Bun/Node runtime queries cpu/cgroup info; non-fatal if blocked but cleaner to allow
	"/etc/ld.so.cache",
	"/etc/resolv.conf",
	"/etc/ssl",
	"/etc/ca-certificates",
	"/etc/nsswitch.conf",
	"/etc/hosts",
	"/etc/host.conf",
	"/etc/gai.conf",
	"/etc/passwd",
	"/etc/group",
	"/etc/localtime",
	"/etc/timezone",
	// /nix/store and Linuxbrew's prefix hold the real binaries that /usr/bin
	// symlinks resolve to on Nix(OS) and Linuxbrew hosts. Stat-probed at use
	// time, so non-Nix / non-Linuxbrew hosts pay nothing.
	"/nix/store",
	"/home/linuxbrew/.linuxbrew",
}

// /dev/pts and /dev/shm are listed even though /dev is present because
// Landlock evaluates rules per mount point and these are typically separate
// devpts/tmpfs mounts.
var linuxSystemWritable = []string{
	"/dev",
	"/dev/pts",
	"/dev/shm",
	"/run",
}

func linuxGrantedPaths(policy Policy) GrantedPathSet {
	return DeriveGrantedPathSet(policy)
}

// linuxLandlockGrantedPaths augments the guard-derived GrantedPathSet with
// the system directories Landlock needs to allow before any process can exec
// or do interactive I/O. bwrap handles its own set, so this is Landlock-only.
func linuxLandlockGrantedPaths(policy Policy) GrantedPathSet {
	gps := DeriveGrantedPathSet(policy)

	if gps.OriginGuard == nil {
		gps.OriginGuard = make(map[string]string)
	}

	for _, p := range linuxSystemReadable {
		if _, err := os.Stat(p); err == nil {
			resolved := filepath.Clean(p)
			if !pathCoveredBy(resolved, gps.Writable, gps.Readable) {
				gps.Readable = append(gps.Readable, resolved)
				gps.OriginGuard[resolved] = "linux:system"
			}
		}
	}

	for _, p := range linuxSystemWritable {
		if _, err := os.Stat(p); err == nil {
			resolved := filepath.Clean(p)
			if !pathCoveredBy(resolved, gps.Writable, nil) {
				gps.Writable = append(gps.Writable, resolved)
				gps.OriginGuard[resolved] = "linux:system-writable"
			}
		}
	}

	return gps
}

func pathCoveredBy(p string, writable, readable []string) bool {
	for _, w := range writable {
		if w == p || strings.HasPrefix(p, w+"/") {
			return true
		}
	}
	for _, r := range readable {
		if r == p || strings.HasPrefix(p, r+"/") {
			return true
		}
	}
	return false
}

// landlockPolicyJSON is the serializable Policy projection passed to the
// __sandbox-apply re-exec. AgentModule is dropped (interface; not JSON-able);
// AgentReadable/Writable carry its resolved per-platform path output.
type landlockPolicyJSON struct {
	Guards          []string    `json:"Guards,omitempty"`
	ProjectRoot     string      `json:"ProjectRoot,omitempty"`
	RuntimeDir      string      `json:"RuntimeDir,omitempty"`
	TempDir         string      `json:"TempDir,omitempty"`
	Network         NetworkMode `json:"Network,omitempty"`
	AllowPorts      []int       `json:"AllowPorts,omitempty"`
	DenyPorts       []int       `json:"DenyPorts,omitempty"`
	SSHPorts        []int       `json:"SSHPorts,omitempty"`
	ExtraDenied     []string    `json:"ExtraDenied,omitempty"`
	ExtraWritable   []string    `json:"ExtraWritable,omitempty"`
	ExtraReadable   []string    `json:"ExtraReadable,omitempty"`
	ExtraAllow      []string    `json:"ExtraAllow,omitempty"`
	AllowSubprocess bool        `json:"AllowSubprocess"`
	CleanEnv        bool        `json:"CleanEnv"`
	AgentReadable   []string    `json:"AgentReadable,omitempty"`
	AgentWritable   []string    `json:"AgentWritable,omitempty"`
}

func policyToJSON(p Policy) landlockPolicyJSON {
	j := landlockPolicyJSON{
		Guards:          p.Guards,
		ProjectRoot:     p.ProjectRoot,
		RuntimeDir:      p.RuntimeDir,
		TempDir:         p.TempDir,
		Network:         p.Network,
		AllowPorts:      p.AllowPorts,
		DenyPorts:       p.DenyPorts,
		SSHPorts:        p.SSHPorts,
		ExtraDenied:     p.ExtraDenied,
		ExtraWritable:   p.ExtraWritable,
		ExtraReadable:   p.ExtraReadable,
		ExtraAllow:      p.ExtraAllow,
		AllowSubprocess: p.AllowSubprocess,
		CleanEnv:        p.CleanEnv,
	}
	if p.AgentModule != nil {
		// Re-evaluate the module so we can serialise its per-platform path
		// grants for the re-exec child (which receives AgentModule=nil after
		// policyFromJSON). The macOS Rules slice is intentionally discarded
		// — the child enforces via Landlock, not Seatbelt.
		homeDir, _ := os.UserHomeDir()
		moduleResult := p.AgentModule.Rules(p.ToSeatbeltContext(homeDir))
		j.AgentReadable = moduleResult.Readable
		j.AgentWritable = moduleResult.Writable
	}
	return j
}

// policyFromJSON inverts policyToJSON. AgentModule stays nil (unused for
// enforcement); AgentReadable / AgentWritable are folded into the policy's
// extra-readable / extra-writable lists.
func policyFromJSON(j landlockPolicyJSON) Policy {
	extraWritable := append([]string{}, j.ExtraWritable...)
	extraWritable = append(extraWritable, j.AgentWritable...)
	return Policy{
		Guards:          j.Guards,
		ProjectRoot:     j.ProjectRoot,
		RuntimeDir:      j.RuntimeDir,
		TempDir:         j.TempDir,
		Network:         j.Network,
		AllowPorts:      j.AllowPorts,
		DenyPorts:       j.DenyPorts,
		SSHPorts:        j.SSHPorts,
		ExtraDenied:     j.ExtraDenied,
		ExtraWritable:   extraWritable,
		ExtraReadable:   append(j.ExtraReadable, j.AgentReadable...),
		ExtraAllow:      j.ExtraAllow,
		AllowSubprocess: j.AllowSubprocess,
		CleanEnv:        j.CleanEnv,
	}
}

// applyLandlock re-execs aide with __sandbox-apply (Landlock can only restrict
// the calling process; the re-exec target self-applies the filter then execs
// the agent).
func (l *LinuxSandbox) applyLandlock(cmd *exec.Cmd, policy Policy, runtimeDir string) error {
	policyJSON := policyToJSON(policy)
	policyBytes, err := json.Marshal(policyJSON)
	if err != nil {
		return fmt.Errorf("marshal sandbox policy: %w", err)
	}

	policyPath := filepath.Join(runtimeDir, "landlock-policy.json")
	if err := os.WriteFile(policyPath, policyBytes, 0600); err != nil {
		return fmt.Errorf("write sandbox policy: %w", err)
	}

	aideBin, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve aide binary: %w", err)
	}

	originalArgs := cmd.Args
	innerArgs := append(
		[]string{"aide", "__sandbox-apply", policyPath, "--"},
		originalArgs...,
	)
	cmd.Path = aideBin
	cmd.Args = innerArgs

	if policy.CleanEnv {
		cmd.Env = filterEnv(cmd.Env)
	}

	return nil
}

// shouldGateNetwork decides whether to enable Landlock network gating.
// landlock.V5.Restrict denies all TCP traffic without explicit rules, so we
// only enable it when the user actually asked for restriction (network=none,
// or outbound with an explicit port allow-set). "outbound, no port rules" and
// "unrestricted" use RestrictPaths so the kernel's normal network is intact.
//
// Limitation: in "outbound, no port rules" we cannot mirror macOS's
// inbound-bind block — Landlock has no wildcard form for ConnectTCP/BindTCP.
func shouldGateNetwork(mode NetworkMode, portPolicy PortPolicyEffective) bool {
	if mode == NetworkNone {
		return true
	}
	if mode == NetworkUnrestricted {
		return false
	}
	return len(portPolicy.AllowSet) > 0
}

// RunSandboxApply is the __sandbox-apply re-exec handler. Runs in the child
// process so Landlock restricts only this process and the agent it execs.
func RunSandboxApply(policyPath string, agentCmd []string) error {
	policyBytes, err := os.ReadFile(policyPath)
	if err != nil {
		return fmt.Errorf("read sandbox policy: %w", err)
	}

	var pj landlockPolicyJSON
	if err := json.Unmarshal(policyBytes, &pj); err != nil {
		return fmt.Errorf("unmarshal sandbox policy: %w", err)
	}
	policy := policyFromJSON(pj)

	// Resolve agent path before Landlock takes effect; LookPath needs FS access.
	agentPath, err := exec.LookPath(agentCmd[0])
	if err != nil {
		return fmt.Errorf("agent not found: %w", err)
	}
	agentPath = filepath.Clean(agentPath)

	gps := linuxLandlockGrantedPaths(policy)

	var rules []landlock.Rule
	for _, p := range gps.Writable {
		if !pathExists(p) {
			continue
		}
		rule := landlock.RWDirs(p)
		// /dev needs ioctl for TIOCGWINSZ/TCGETS on tty devices; RWDirs omits it.
		if p == "/dev" || strings.HasPrefix(p, "/dev/") {
			rule = rule.WithIoctlDev()
		}
		rules = append(rules, rule)
	}

	// Both the agent symlink and its resolved target must be readable for execve.
	agentExecPaths := collectAgentExecPaths(agentPath)
	allReadable := appendMissingPaths(gps.Readable, gps.Writable, agentExecPaths)

	for _, p := range allReadable {
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		if info.IsDir() {
			rules = append(rules, landlock.RODirs(p))
		} else {
			rules = append(rules, landlock.ROFiles(p))
		}
	}

	// Validate raw ports — DerivePortPolicy silently drops out-of-range values.
	if err := ValidatePortRange(policy.AllowPorts); err != nil {
		return fmt.Errorf("port policy: %w", err)
	}
	if err := ValidatePortRange(policy.DenyPorts); err != nil {
		return fmt.Errorf("port policy: %w", err)
	}
	caps := DetectKernelCapabilities()
	portPolicy := DerivePortPolicy(policy, caps.LandlockABI >= 4)

	cfg := landlock.V5.BestEffort()
	if shouldGateNetwork(policy.Network, portPolicy) {
		for _, port := range portPolicy.AllowSet {
			if port >= 0 && port <= 65535 {
				rules = append(rules, landlock.ConnectTCP(uint16(port)))
			}
		}
		if err := cfg.Restrict(rules...); err != nil {
			return fmt.Errorf("landlock restrict: %w", err)
		}
	} else {
		if err := cfg.RestrictPaths(rules...); err != nil {
			return fmt.Errorf("landlock restrict-paths: %w", err)
		}
	}

	return syscall.Exec(agentPath, agentCmd, os.Environ())
}

// GenerateProfile returns a human-readable text describing the sandbox profile
// that would be applied for the given policy (tier, paths, port policy).
func (l *LinuxSandbox) GenerateProfile(policy Policy) (string, error) {
	var b strings.Builder

	tier := PlatformIsolationTier(policy)

	fmt.Fprintf(&b, "# Tier: %s\n", tier.Tier)
	fmt.Fprintf(&b, "# Backend: %s\n", tier.Backend)
	if tier.Reason != "" {
		fmt.Fprintf(&b, "# Reason: %s\n", tier.Reason)
	}
	fmt.Fprintf(&b, "# Port filtering: %s\n\n", tier.PortFiltering)

	gps := linuxGrantedPaths(policy)
	writable := gps.Writable
	readable := gps.Readable
	denied := gps.Denied

	b.WriteString("# Linux Sandbox Profile\n\n")

	b.WriteString("## Writable paths\n")
	for _, p := range writable {
		fmt.Fprintf(&b, "  %s\n", p)
	}

	b.WriteString("\n## Readable paths\n")
	for _, p := range readable {
		fmt.Fprintf(&b, "  %s\n", p)
	}

	deniedPaths := expandGlobs(denied)
	if len(deniedPaths) > 0 {
		b.WriteString("\n## Denied paths\n")
		for _, p := range deniedPaths {
			fmt.Fprintf(&b, "  %s\n", p)
		}
		if len(deniedPaths) != len(denied) {
			b.WriteString("\n  # (expanded from globs in denied list)\n")
		}
	}

	caps := DetectKernelCapabilities()
	portPolicy := DerivePortPolicy(policy, caps.LandlockABI >= 4)
	fmt.Fprintf(&b, "\n## Network: %s\n", policy.Network)
	fmt.Fprintf(&b, "## Port policy mode: %s\n", portPolicy.Mode)
	if len(portPolicy.AllowSet) > 0 {
		b.WriteString("## Allow ports:")
		for _, port := range portPolicy.AllowSet {
			fmt.Fprintf(&b, " %d", port)
		}
		b.WriteString("\n")
	}
	if !portPolicy.Enforceable {
		b.WriteString("## Warning: port filtering not enforceable with current backend\n")
	}

	fmt.Fprintf(&b, "\n## Allow subprocess: %v\n", policy.AllowSubprocess)
	fmt.Fprintf(&b, "## Clean env: %v\n", policy.CleanEnv)

	return b.String(), nil
}

// applyBwrap wraps the command with bubblewrap for filesystem isolation.
func (l *LinuxSandbox) applyBwrap(cmd *exec.Cmd, policy Policy, bwrapPath string) error {
	var bwrapArgs []string

	gps := linuxGrantedPaths(policy)
	writable := gps.Writable
	readable := gps.Readable
	denied := gps.Denied

	// Writable paths: --bind-try src src
	for _, p := range writable {
		bwrapArgs = append(bwrapArgs, "--bind-try", p, p)
	}

	// Readable paths: --ro-bind-try src src
	for _, p := range readable {
		bwrapArgs = append(bwrapArgs, "--ro-bind-try", p, p)
	}

	// System essentials
	bwrapArgs = append(bwrapArgs,
		"--ro-bind", "/usr", "/usr",
		"--ro-bind", "/etc/resolv.conf", "/etc/resolv.conf",
		"--proc", "/proc",
		"--dev", "/dev",
		"--tmpfs", "/tmp",
	)

	// /nix/store and Linuxbrew prefix hold the real binaries /usr/bin
	// symlinks resolve to on Nix(OS) and Linuxbrew hosts.
	for _, p := range []string{"/lib", "/lib64", "/nix/store", "/home/linuxbrew/.linuxbrew"} {
		if _, err := os.Stat(p); err == nil {
			bwrapArgs = append(bwrapArgs, "--ro-bind", p, p)
		}
	}

	// Denied paths: mask with empty tmpfs
	for _, p := range expandGlobs(denied) {
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			bwrapArgs = append(bwrapArgs, "--tmpfs", p)
		}
		// For files, the parent dir restriction handles it
	}

	// Network isolation
	if policy.Network == NetworkNone {
		bwrapArgs = append(bwrapArgs, "--unshare-net")
	}

	// Port-level filtering is not supported by bwrap -- log a warning
	if len(policy.AllowPorts) > 0 || len(policy.DenyPorts) > 0 {
		fmt.Fprintln(os.Stderr, "aide: warning: Port-level filtering not supported by bwrap; using mode-only network policy")
	}

	// Subprocess control
	// bwrap has no direct subprocess gate; PID-namespace isolation is the
	// closest equivalent.
	if !policy.AllowSubprocess {
		bwrapArgs = append(bwrapArgs, "--unshare-pid")
	}

	// Append -- and the original command
	bwrapArgs = append(bwrapArgs, "--")
	bwrapArgs = append(bwrapArgs, cmd.Args...)

	// Rewrite the command
	cmd.Path = bwrapPath
	cmd.Args = append([]string{"bwrap"}, bwrapArgs...)

	if policy.CleanEnv {
		cmd.Env = filterEnv(cmd.Env)
	}

	return nil
}

// pathExists is a stat-only existence probe. Landlock fails at restrict time
// for non-existent paths, so callers skip those.
func pathExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// collectAgentExecPaths returns the symlink directory, the symlink itself, and
// the resolved target with its directory — all needed for execve under Landlock.
func collectAgentExecPaths(agentPath string) []string {
	candidates := []string{
		filepath.Dir(agentPath),
		agentPath,
	}
	if resolved, err := filepath.EvalSymlinks(agentPath); err == nil && resolved != agentPath {
		candidates = append(candidates, resolved, filepath.Dir(resolved))
	}
	return candidates
}

func appendMissingPaths(readable, writable, candidates []string) []string {
	covered := func(p string) bool {
		for _, w := range writable {
			if w == p || strings.HasPrefix(p, w+"/") {
				return true
			}
		}
		for _, r := range readable {
			if r == p || strings.HasPrefix(p, r+"/") {
				return true
			}
		}
		return false
	}
	result := readable
	for _, c := range candidates {
		if c != "" && !covered(c) {
			result = append(result, c)
		}
	}
	return result
}

// filterEnv and expandGlobs are in sandbox.go (shared across platforms).
