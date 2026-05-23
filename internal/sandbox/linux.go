//go:build linux

// Package sandbox implements OS-native sandboxing for agent processes.
package sandbox

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/jskswamy/aide/pkg/seatbelt"
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
// AgentReadable/Writable carry its resolved LinuxPathProvider output.
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
	AgentReadable            []string `json:"AgentReadable,omitempty"`
	AgentWritable            []string `json:"AgentWritable,omitempty"`
	AgentAtomicWritableFiles []string `json:"AgentAtomicWritableFiles,omitempty"`
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
	homeDir, _ := os.UserHomeDir()
	ctx := p.ToSeatbeltContext(homeDir)
	if p.AgentModule != nil {
		// Re-evaluate the module so we can serialise its Linux-specific
		// path grants for the re-exec child (which receives AgentModule=nil
		// after policyFromJSON). The Rules() call is the same one
		// EvaluateGuards already makes in the parent; the macOS Rules
		// slice is intentionally discarded — the child enforces via
		// Landlock, not Seatbelt.
		moduleResult := p.AgentModule.Rules(ctx)
		j.AgentReadable = moduleResult.Readable
		j.AgentWritable = moduleResult.Writable
	}
	if lap, ok := p.AgentModule.(seatbelt.LinuxAtomicWriteProvider); ok {
		j.AgentAtomicWritableFiles = lap.LinuxAtomicWritableFiles(ctx)
	}
	return j
}

// policyFromJSON inverts policyToJSON. AgentModule stays nil (unused for
// enforcement). When AgentAtomicWritableFiles is non-empty the parent dirs are
// added to ExtraWritable so Landlock permits the per-file tmpfs overlay.
func policyFromJSON(j landlockPolicyJSON) Policy {
	extraWritable := append([]string{}, j.ExtraWritable...)
	extraWritable = append(extraWritable, j.AgentWritable...)
	extraWritable = append(extraWritable, uniqueExistingParents(j.AgentAtomicWritableFiles)...)
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

// applyLandlock re-execs aide with __sandbox-apply (Landlock self-sandboxes
// the caller; we cannot apply it to a child directly). When the policy declares
// atomic-writable files, the command is additionally wrapped with bwrap to set
// up a per-file tmpfs overlay so the agent's atomic-rename pattern works
// without granting broad write access to $HOME.
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

	if needsAtomicWriteOverlay(policyJSON) {
		bwrapPath, lookErr := exec.LookPath("bwrap")
		if lookErr != nil {
			return fmt.Errorf("agent declares atomic-writable files but bwrap is not installed; install bwrap to use this agent")
		}
		if missing := missingAtomicWritableFiles(policyJSON); len(missing) > 0 {
			return fmt.Errorf(
				"agent declares atomic-writable files that do not exist: %s; "+
					"run the agent once outside the sandbox to initialize its config, then retry",
				strings.Join(missing, ", "),
			)
		}
		homeDir, _ := os.UserHomeDir()
		layout, err := setupOverlayLayout(runtimeDir, homeDir,
			policyJSON.AgentAtomicWritableFiles, policyJSON.AgentReadable)
		if err != nil {
			return fmt.Errorf("overlay setup: %w", err)
		}
		overlayArgs := buildOverlayBwrapArgs(
			layout, homeDir,
			policyJSON.AgentAtomicWritableFiles,
			policyJSON.AgentReadable,
			policyJSON.AgentWritable,
			policyJSON.AllowSubprocess,
			policyJSON.Network,
		)

		// Outer wrapper: aide __sandbox-sync waits for the bwrap chain,
		// then syncs the upper layer's modified atomic files back to host.
		syncArgs := []string{
			"__sandbox-sync",
			"--upper", layout.Upper,
			"--home", homeDir,
			"--overlay-root", layout.Root,
		}
		for _, f := range policyJSON.AgentAtomicWritableFiles {
			syncArgs = append(syncArgs, "--sync-file", f)
		}
		syncArgs = append(syncArgs, "--")

		fullArgs := append([]string{"aide"}, syncArgs...)
		fullArgs = append(fullArgs, bwrapPath)
		fullArgs = append(fullArgs, overlayArgs...)
		fullArgs = append(fullArgs, "--")
		fullArgs = append(fullArgs, aideBin)
		fullArgs = append(fullArgs, innerArgs[1:]...)
		cmd.Path = aideBin
		cmd.Args = fullArgs
		if policy.CleanEnv {
			cmd.Env = filterEnv(cmd.Env)
		}
		return nil
	}

	cmd.Path = aideBin
	cmd.Args = innerArgs

	// Pure Landlock path (no bwrap wrapper). The seccomp filter installed
	// in RunSandboxApply blocks subprocess syscalls; CLONE_NEWPID adds PID
	// namespace isolation as defence in depth so any future bypass of the
	// seccomp filter still cannot see host processes.
	if !policy.AllowSubprocess {
		if cmd.SysProcAttr == nil {
			cmd.SysProcAttr = &syscall.SysProcAttr{}
		}
		cmd.SysProcAttr.Cloneflags |= syscall.CLONE_NEWPID
	}

	if policy.CleanEnv {
		cmd.Env = filterEnv(cmd.Env)
	}

	return nil
}

// atomicWritableFiles is the bwrap-path counterpart of policyToJSON's
// LinuxAtomicWriteProvider assertion. applyBwrap uses it to fail fast before
// launching a sandbox that cannot honour the atomic-rename contract.
func atomicWritableFiles(policy Policy) []string {
	if policy.AgentModule == nil {
		return nil
	}
	lap, ok := policy.AgentModule.(seatbelt.LinuxAtomicWriteProvider)
	if !ok {
		return nil
	}
	ctx := policy.ToSeatbeltContext(homeDirOrEmpty())
	var out []string
	for _, f := range lap.LinuxAtomicWritableFiles(ctx) {
		if strings.TrimSpace(f) != "" {
			out = append(out, f)
		}
	}
	return out
}

func homeDirOrEmpty() string {
	h, _ := os.UserHomeDir()
	return h
}

func needsAtomicWriteOverlay(j landlockPolicyJSON) bool {
	for _, f := range j.AgentAtomicWritableFiles {
		if strings.TrimSpace(f) != "" {
			return true
		}
	}
	return false
}

// missingAtomicWritableFiles returns declared files absent from disk. bwrap
// cannot bind-mount a non-existent path, and the agent's atomic-rename would
// otherwise land in the overlay and be lost on sandbox exit.
func missingAtomicWritableFiles(j landlockPolicyJSON) []string {
	var missing []string
	for _, f := range j.AgentAtomicWritableFiles {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		if !pathExists(f) {
			missing = append(missing, f)
		}
	}
	return missing
}

// uniqueExistingParents returns the unique parent directories of the given
// files, keeping only those that currently exist on disk. policyFromJSON uses
// this to mark atomic-writable file parents as writable for Landlock, since
// the rename(2) at the parent dir's inode needs WRITE/MAKE_REG/REMOVE_FILE.
func uniqueExistingParents(files []string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, f := range files {
		if strings.TrimSpace(f) == "" {
			continue
		}
		parent := filepath.Clean(filepath.Dir(f))
		if seen[parent] {
			continue
		}
		if !pathExists(parent) {
			continue
		}
		seen[parent] = true
		out = append(out, parent)
	}
	return out
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

	// Block subprocess creation via seccomp, installed AFTER Landlock so a
	// failure here doesn't leave the filesystem-restriction half applied.
	// The filter survives execve so the agent inherits it.
	if !policy.AllowSubprocess {
		if err := installNoSubprocessSeccomp(); err != nil {
			return fmt.Errorf("install seccomp: %w", err)
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

func (l *LinuxSandbox) applyBwrap(cmd *exec.Cmd, policy Policy, bwrapPath string) error {
	// Atomic-rename contract is only honoured by the Landlock+bwrap overlay
	// path. The bwrap-only fallback would expose the declared files via
	// --ro-bind-try (EBUSY on rename) or accept the rename into a throwaway
	// tmpfs (silent data loss). Refuse to launch.
	if atomic := atomicWritableFiles(policy); len(atomic) > 0 {
		return fmt.Errorf(
			"sandbox: agent declares atomic-writable files (%s) but Landlock is unavailable; "+
				"the bwrap-only fallback cannot honour the atomic-rename contract and would silently drop these writes. "+
				"Upgrade to a kernel with Landlock enabled (≥ 5.13, ABI ≥ 4 for full enforcement) or disable the agent's atomic-write declarations",
			strings.Join(atomic, ", "),
		)
	}

	var bwrapArgs []string

	gps := linuxGrantedPaths(policy)
	writable := gps.Writable
	readable := gps.Readable
	denied := gps.Denied

	for _, p := range writable {
		bwrapArgs = append(bwrapArgs, "--bind-try", p, p)
	}
	for _, p := range readable {
		bwrapArgs = append(bwrapArgs, "--ro-bind-try", p, p)
	}

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

	// Mask denied directories; files are handled via parent restriction.
	for _, p := range expandGlobs(denied) {
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			bwrapArgs = append(bwrapArgs, "--tmpfs", p)
		}
	}

	if policy.Network == NetworkNone {
		bwrapArgs = append(bwrapArgs, "--unshare-net")
	}

	if len(policy.AllowPorts) > 0 || len(policy.DenyPorts) > 0 {
		fmt.Fprintln(os.Stderr, "aide: warning: Port-level filtering not supported by bwrap; using mode-only network policy")
	}

	// Subprocess gate. Layered: --unshare-pid bounds the blast radius via
	// PID-namespace isolation (children only see siblings in the namespace);
	// --seccomp installs a BPF filter that blocks the underlying
	// clone/fork/vfork syscalls outright so subprocess creation actually
	// fails rather than just being hidden.
	if !policy.AllowSubprocess {
		bwrapArgs = append(bwrapArgs, "--unshare-pid")
		memFile, err := noSubprocessSeccompMemfd()
		if err != nil {
			return fmt.Errorf("seccomp setup: %w", err)
		}
		// Each ExtraFiles[i] becomes fd 3+i in the child.
		childFD := 3 + len(cmd.ExtraFiles)
		cmd.ExtraFiles = append(cmd.ExtraFiles, memFile)
		bwrapArgs = append(bwrapArgs, "--seccomp", strconv.Itoa(childFD))
	}

	bwrapArgs = append(bwrapArgs, "--")
	bwrapArgs = append(bwrapArgs, cmd.Args...)

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
