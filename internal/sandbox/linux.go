//go:build linux

// Package sandbox implements OS-native sandboxing for agent processes.
package sandbox

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/landlock-lsm/go-landlock/landlock"
	"golang.org/x/sys/unix"
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

	// Dispatch on the backend ComputeIsolationTier selected, not on raw
	// capability flags. The tier resolver may pick bwrap over a
	// kernel-present-but-degraded Landlock when the requested policy is
	// something bwrap can actually enforce (e.g. network=none on ABI < 4).
	switch tier.Backend {
	case BackendLandlock:
		if tier.Tier == TierDegraded {
			hasPortRules := len(policy.AllowPorts) > 0 || len(policy.DenyPorts) > 0
			remedy := "upgrade to kernel ≥ 6.7 (Landlock ABI 4), install bubblewrap, or set network to unrestricted"
			if hasPortRules {
				remedy = "upgrade to kernel ≥ 6.7 (Landlock ABI 4) or remove port rules"
			}
			return fmt.Errorf("sandbox: %s; %s", tier.Reason, remedy)
		}
		return l.applyLandlock(cmd, policy, runtimeDir)
	case BackendBwrap:
		bwrapPath, err := exec.LookPath("bwrap")
		if err != nil {
			return fmt.Errorf("sandbox: tier selected bubblewrap but `bwrap` not found on PATH: %w", err)
		}
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
	"/sys/fs/cgroup", // Bun/Node runtime queries cgroup membership; scoped to avoid exposing
	// /sys/class/net, /sys/bus/usb, /sys/kernel/security, and debugfs.
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

	deniedSet := make(map[string]bool, len(gps.Denied))
	for _, d := range gps.Denied {
		deniedSet[d] = true
	}

	for _, p := range linuxSystemReadable {
		if _, err := os.Stat(p); err == nil {
			// Use resolveSymlink so the resolved path matches the symlink-resolved
			// entries in deniedSet. On merged-usr Linux (Ubuntu 22.04+, Fedora 36+)
			// /bin, /lib, /lib64, /sbin are symlinks to /usr/{bin,lib,…}. Using
			// filepath.Clean would produce "/bin" while deniedSet holds "/usr/bin",
			// letting a Landlock RODirs("/bin") rule grant access to the denied
			// /usr subtree at enforcement time (kernel follows the symlink).
			resolved := resolveSymlink(p)
			if resolved == "" {
				resolved = filepath.Clean(p)
			}
			// Honour deny-wins: a guard or config that explicitly denied this
			// path (or any ancestor directory it lives under) must not be
			// overridden by the system-path bootstrap list.
			if isUnderDeniedTree(resolved, deniedSet) {
				continue
			}
			if !pathCoveredBy(resolved, gps.Writable, gps.Readable) {
				gps.Readable = append(gps.Readable, resolved)
				gps.OriginGuard[resolved] = "linux:system"
			}
		}
	}

	for _, p := range linuxSystemWritable {
		if _, err := os.Stat(p); err == nil {
			resolved := resolveSymlink(p)
			if resolved == "" {
				resolved = filepath.Clean(p)
			}
			if isUnderDeniedTree(resolved, deniedSet) {
				continue
			}
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
// Env must be included so that capability guards in the re-exec child can
// evaluate env-var-dependent paths (e.g. KUBECONFIG, AWS_CONFIG_FILE,
// DOCKER_CERT_PATH) identically to the parent — omitting it causes those
// guards to fall back to HOME-based defaults and their actual paths are
// absent from the Landlock allow-list, silently denying agent reads.
type landlockPolicyJSON struct {
	Guards          []string    `json:"Guards,omitempty"`
	HomeDir         string      `json:"HomeDir,omitempty"`
	Env             []string    `json:"Env,omitempty"`
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

func policyToJSON(p Policy) (landlockPolicyJSON, error) {
	j := landlockPolicyJSON{
		Guards:          p.Guards,
		HomeDir:         p.HomeDir,
		Env:             p.Env,
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
		//
		// HomeDir is required: every Linux agent module derives its
		// writable/readable paths from $HOME (e.g. Claude's CLAUDE_CONFIG_DIR
		// under ~/.config/aide/claude). Silently swallowing the error here
		// would yield j.AgentWritable == nil, which means the re-exec child
		// enforces a Landlock allow-list with no agent-config dir at all —
		// every subsequent agent write is then dropped by the kernel with no
		// user-visible diagnostic. Fail fast instead.
		if p.HomeDir == "" {
			return landlockPolicyJSON{}, fmt.Errorf("resolve $HOME for agent module %q: HomeDir not set on policy", p.AgentModule.Name())
		}
		moduleResult := p.AgentModule.Rules(p.ToSeatbeltContext())
		j.AgentReadable = moduleResult.Allowed
		j.AgentWritable = moduleResult.Writable
	}
	return j, nil
}

// policyFromJSON inverts policyToJSON. AgentModule stays nil (unused for
// enforcement); AgentReadable / AgentWritable are folded into the policy's
// extra-readable / extra-writable lists.
func policyFromJSON(j landlockPolicyJSON) Policy {
	extraWritable := append([]string{}, j.ExtraWritable...)
	extraWritable = append(extraWritable, j.AgentWritable...)
	extraReadable := append([]string{}, j.ExtraReadable...)
	extraReadable = append(extraReadable, j.AgentReadable...)
	return Policy{
		Guards:          j.Guards,
		HomeDir:         j.HomeDir,
		Env:             j.Env,
		ProjectRoot:     j.ProjectRoot,
		RuntimeDir:      j.RuntimeDir,
		TempDir:         j.TempDir,
		Network:         j.Network,
		AllowPorts:      j.AllowPorts,
		DenyPorts:       j.DenyPorts,
		SSHPorts:        j.SSHPorts,
		ExtraDenied:     j.ExtraDenied,
		ExtraWritable:   extraWritable,
		ExtraReadable:   extraReadable,
		ExtraAllow:      j.ExtraAllow,
		AllowSubprocess: j.AllowSubprocess,
		CleanEnv:        j.CleanEnv,
	}
}

// applyLandlock re-execs aide with __sandbox-apply (Landlock can only restrict
// the calling process; the re-exec target self-applies the filter then execs
// the agent).
func (l *LinuxSandbox) applyLandlock(cmd *exec.Cmd, policy Policy, _ string) error {
	policyJSON, err := policyToJSON(policy)
	if err != nil {
		return fmt.Errorf("build sandbox policy: %w", err)
	}
	policyBytes, err := json.Marshal(policyJSON)
	if err != nil {
		return fmt.Errorf("marshal sandbox policy: %w", err)
	}

	policyFD, err := writePolicyToMemfd(policyBytes)
	if err != nil {
		return err
	}

	aideBin, err := os.Executable()
	if err != nil {
		_ = policyFD.Close()
		return fmt.Errorf("resolve aide binary: %w", err)
	}

	// Pass the kernel-allocated fd number explicitly in argv. cmd.ExtraFiles
	// is irrelevant at runtime — the launcher uses raw syscall.Exec which
	// does not honour it — but we still attach the *os.File there to keep a
	// Go-level reference alive (preventing the runtime finalizer from closing
	// the fd before exec) and to give tests a stable introspection handle.
	policyFDNum := int(policyFD.Fd())
	cmd.ExtraFiles = []*os.File{policyFD}

	originalArgs := cmd.Args
	innerArgs := append(
		[]string{"aide", "__sandbox-apply", fmt.Sprintf("--policy-fd=%d", policyFDNum), "--"},
		originalArgs...,
	)
	cmd.Path = aideBin
	cmd.Args = innerArgs

	if policy.CleanEnv {
		cmd.Env = filterEnv(cmd.Env, policy)
	}

	return nil
}

// shouldGateNetwork decides whether to enable Landlock network gating.
// landlock.V5.Restrict denies all TCP traffic without explicit rules, so the
// gate is on whenever the user expressed restriction intent — network=none,
// or outbound with any non-unrestricted port policy. Gating on
// portPolicy.Mode (not len(AllowSet)) is intentional: when DenyPorts covers
// every port, DerivePortPolicy returns Mode="deny_complement" with an empty
// AllowSet, and the user's deny-all-ports intent must still produce
// "no outbound TCP" rather than silently passing all traffic through.
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
	return portPolicy.Mode != "unrestricted"
}

// writePolicyToMemfd writes policyBytes to an anonymous in-memory file
// (memfd_create) and returns an *os.File whose fd is suitable for inheritance
// across syscall.Exec.
//
// MFD_CLOEXEC is intentionally NOT set. The production launcher invokes
// SyscallExecer.Exec → raw syscall.Exec, which does NOT honour cmd.ExtraFiles
// (that is a *exec.Cmd.Start-only abstraction). The only way to deliver the
// memfd to the re-exec child is via fd inheritance, which requires
// FD_CLOEXEC to be clear at exec time. The caller is expected to encode the
// kernel-allocated fd number in argv (e.g. --policy-fd=N) so the child can
// locate it without assuming a fixed fd.
//
// Confidentiality is preserved by the kernel-backed memfd never touching disk
// and being reclaimed once both ends close.
//
// Note we deliberately do NOT dup the fd onto a fixed low number such as 3.
// In Go test processes, fd 3 is occupied by the test framework's testlogfile;
// dup2'ing onto it silently breaks subsequent tests. Letting the kernel pick
// the fd avoids that collision and is sufficient because the fd number is
// passed explicitly in argv.
//
// CRITICAL — Pinned against GC: os.NewFile installs a runtime finalizer
// (on its unexported *file inner struct) that calls Close(). Once the
// launcher extracts cmd.Path/Args/Env and drops the cmd reference, the
// *os.File becomes unreachable; the finalizer can fire during the
// allocation-heavy work that follows (banner rendering, signal setup) and
// silently close fd N. The re-exec child then sees EBADF on read, with the
// symptom "read landlock-policy: bad file descriptor" appearing only
// sometimes (whenever GC happens to run in the race window).
//
// runtime.SetFinalizer(f, nil) does NOT help here — the finalizer lives on
// the unexported f.file pointer, not on f itself. The only reliable way to
// suppress it is to keep the *os.File reachable. We do that by appending
// it to pinnedSandboxMemfds; the launcher process is short-lived (it
// syscall.Exec's into the re-exec child shortly after this returns), so
// the pin is reclaimed by process image replacement and not a real leak.
func writePolicyToMemfd(policyBytes []byte) (*os.File, error) {
	fd, err := unix.MemfdCreate("landlock-policy", 0)
	if err != nil {
		return nil, fmt.Errorf("memfd_create for sandbox policy: %w", err)
	}
	f := os.NewFile(uintptr(fd), "landlock-policy")
	if _, err := f.Write(policyBytes); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("write sandbox policy to memfd: %w", err)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("seek sandbox policy memfd: %w", err)
	}
	pinSandboxMemfd(f)
	return f, nil
}

// pinnedSandboxMemfds keeps *os.File references alive past the launcher's
// drop of cmd.ExtraFiles, suppressing the runtime finalizer that would
// otherwise close the kernel fd before syscall.Exec inherits it. See the
// CRITICAL comment in writePolicyToMemfd for details.
//
// The slice is intentionally append-only and never read from; its purpose is
// reachability, not retrieval. It is freed by syscall.Exec replacing the
// process image, or by process exit on error paths.
var pinnedSandboxMemfds []*os.File

func pinSandboxMemfd(f *os.File) {
	pinnedSandboxMemfds = append(pinnedSandboxMemfds, f)
}

// RunSandboxApply is the __sandbox-apply re-exec handler. Runs in the child
// process so Landlock restricts only this process and the agent it execs.
func RunSandboxApply(policyFDStr string, agentCmd []string) error {
	fdNum, err := strconv.Atoi(policyFDStr)
	if err != nil {
		return fmt.Errorf("invalid --policy-fd value %q: %w", policyFDStr, err)
	}
	policyFile := os.NewFile(uintptr(fdNum), "landlock-policy")
	policyBytes, err := io.ReadAll(policyFile)
	_ = policyFile.Close()
	if err != nil {
		return fmt.Errorf("read sandbox policy from fd %d: %w", fdNum, err)
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
		// WithRefer adds LANDLOCK_ACCESS_FS_REFER, which is required for any
		// rename or hard-link operation whose source dirfd differs from the
		// destination dirfd — even within the same writable subtree on the
		// same filesystem. Without it, the kernel returns synthetic EXDEV.
		// This breaks ordinary tooling that relies on cross-directory
		// renames (cargo/rustc rmeta staging, npm/pnpm pack, git temp-rename
		// commits, atomic-write-via-tempfile patterns), so we grant REFER on
		// every writable directory rule. REFER is safe to grant: the kernel
		// still verifies that the destination's parent has at least the same
		// access rights as the source's parent, so it cannot be used to
		// escape the sandbox.
		rule := landlock.RWDirs(p).WithRefer()
		// /dev needs ioctl for TIOCGWINSZ/TCGETS on tty devices; RWDirs omits it.
		if p == "/dev" || strings.HasPrefix(p, "/dev/") {
			rule = rule.WithIoctlDev()
		}
		rules = append(rules, rule)
	}

	// Both the agent symlink and its resolved target must be readable for execve.
	// When AllowSubprocess=false, Phase 2 re-execs the aide binary inside the
	// new PID namespace; that binary must also be in the Landlock allow-list or
	// ForkExec returns EACCES for non-/usr installs (e.g. ~/go/bin/aide).
	execPaths := collectAgentExecPaths(agentPath)
	if !policy.AllowSubprocess {
		aideBin, aerr := os.Executable()
		if aerr != nil {
			return fmt.Errorf("resolve aide executable path for Landlock allow-list: %w", aerr)
		}
		execPaths = append(execPaths, collectAgentExecPaths(filepath.Clean(aideBin))...)
	}
	allReadable := appendMissingPaths(gps.Readable, gps.Writable, execPaths)

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

	// Filesystem restriction — its own ruleset (~50 rules, well under the
	// per-ruleset kernel limit of 65536).
	if err := cfg.RestrictPaths(rules...); err != nil {
		return fmt.Errorf("landlock restrict-paths: %w", err)
	}

	// Network restriction — separate ruleset so filesystem rules and network
	// rules never share a ruleset. deny_complement generates up to 65534
	// ConnectTCP rules; combined with ~50 filesystem rules in a single ruleset
	// that would exceed the kernel's 65536-rule-per-ruleset ceiling (EMFILE).
	// Calling RestrictNet separately keeps each ruleset well within the limit.
	// RestrictNet with zero rules (NetworkNone) blocks all TCP connections.
	if shouldGateNetwork(policy.Network, portPolicy) {
		var portRules []landlock.Rule
		for _, port := range portPolicy.AllowSet {
			portRules = append(portRules, landlock.ConnectTCP(port))
		}
		if err := cfg.RestrictNet(portRules...); err != nil {
			return fmt.Errorf("landlock restrict-net: %w", err)
		}
	}

	if !policy.AllowSubprocess {
		// Fork into a new PID namespace so the agent and all its descendants
		// are isolated from host processes. The child (__sandbox-exec) installs
		// the seccomp filter before exec-ing the agent; seccomp must be
		// installed INSIDE the new namespace so this process (the parent,
		// which still needs to fork) is not itself restricted. Landlock is
		// inherited across fork and execve so the child is already restricted.
		return forkExecInPIDNamespace(agentPath, agentCmd)
	}

	return syscall.Exec(agentPath, agentCmd, os.Environ())
}

// forkExecInPIDNamespace forks into a new PID namespace, runs
// __sandbox-exec (which installs seccomp and execs the agent), waits for
// it, and exits with the same code/signal. Seccomp is NOT installed on
// this (parent) process so that the fork itself succeeds; the child installs
// it before replacing itself with the agent.
//
// Architectural note — why syscall.ForkExec here does not violate the
// single-exec-point rule enforced by internal/launcher:
//
// The parent aide process reaches the launcher's Execer.Exec call exactly
// once. That call does syscall.Exec (replacing the process image) with
// aide __sandbox-apply as the target, which lands in RunSandboxApply.
// forkExecInPIDNamespace is called from RunSandboxApply — i.e. it runs
// inside the already-exec'd __sandbox-apply child, not inside the original
// launcher. The parent aide process no longer exists at this point.
//
// Consequence for future pre-exec hooks: any hook registered in
// internal/launcher fires before Execer.Exec and therefore runs before
// __sandbox-apply is ever invoked. It will NOT fire again for the
// grandchild (agent) spawned here. That is intentional: sandbox setup,
// seccomp, and Landlock are all in-process operations that must happen
// inside the re-exec child; they are not subject to launcher-level hooks.
func forkExecInPIDNamespace(agentPath string, agentCmd []string) error {
	aideBin, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve aide binary for PID namespace fork: %w", err)
	}

	// Phase 2 of __sandbox-apply: args[0] == "--" signals the seccomp+exec
	// path. The resolved agent path is passed separately so the child doesn't
	// need LookPath (which may fail under Landlock restriction), and the
	// original agentCmd is preserved intact so argv[0] in the exec'd agent
	// matches what the AllowSubprocess=true branch produces.
	execArgs := buildPhase2ExecArgs(agentPath, agentCmd)

	pid, err := syscall.ForkExec(aideBin, execArgs, &syscall.ProcAttr{
		Env:   os.Environ(),
		Files: []uintptr{0, 1, 2}, // inherit stdin/stdout/stderr
		Sys:   &syscall.SysProcAttr{Cloneflags: syscall.CLONE_NEWPID},
	})
	if err != nil {
		if err == syscall.EPERM {
			// CLONE_NEWPID without a user namespace requires CAP_SYS_ADMIN,
			// which unprivileged users don't have. Fall back to seccomp-only
			// enforcement: the child installs the no-subprocess filter and
			// execs the agent without PID namespace isolation. Subprocess
			// creation is still blocked by seccomp; only namespace containment
			// is lost.
			fmt.Fprintf(os.Stderr, "aide: sandbox: CLONE_NEWPID unavailable (EPERM), using seccomp-only subprocess enforcement\n")
			return syscall.Exec(aideBin, execArgs, os.Environ())
		}
		return fmt.Errorf("fork into PID namespace: %w", err)
	}

	// Forward signals from this process to the child. Subscribe only to
	// forwarding-relevant signals; subscribing to all signals with an
	// unbuffered-per-signal channel can cause drops under high signal load
	// (e.g. a burst of SIGCHLD from other goroutines before Wait drains the
	// channel). SIGCHLD is intentionally excluded — it is not meaningful for
	// the child process and is handled by Wait4.
	sigCh := make(chan os.Signal, 32)
	signal.Notify(sigCh,
		syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP,
		syscall.SIGUSR1, syscall.SIGUSR2,
		syscall.SIGWINCH, syscall.SIGCONT, syscall.SIGTSTP,
	)
	go func() {
		for sig := range sigCh {
			if s, ok := sig.(syscall.Signal); ok {
				// ESRCH: child already exited before Wait4 drained the channel — harmless.
				// EPERM: unexpected; would mean the PID namespace boundary blocked the signal,
				// but forkExecInPIDNamespace is the init process of the new namespace so Kill
				// should always succeed while the child lives. Both cases are non-actionable
				// here: the signal loop is best-effort forwarding and Wait4 will surface the
				// true exit status regardless.
				_ = syscall.Kill(pid, s)
			}
		}
	}()

	var wstatus syscall.WaitStatus
	for {
		_, werr := syscall.Wait4(pid, &wstatus, 0, nil)
		if werr == nil {
			break
		}
		if werr == syscall.EINTR {
			continue
		}
		signal.Stop(sigCh)
		close(sigCh)
		return fmt.Errorf("wait for sandboxed agent: %w", werr)
	}

	signal.Stop(sigCh)
	close(sigCh)

	if wstatus.Exited() {
		os.Exit(wstatus.ExitStatus())
	}
	if wstatus.Signaled() {
		// Reset disposition to SIG_DFL first so the kernel terminates this
		// process synchronously, then block to avoid os.Exit racing with
		// the still-async delivery. Without this, the parent sometimes
		// sees WIFEXITED(1) instead of WIFSIGNALED(sig), which shells
		// and supervisors use to distinguish crashes from clean exits.
		sig := wstatus.Signal()
		signal.Reset(sig)
		_ = syscall.Kill(os.Getpid(), sig)
		select {}
	}
	os.Exit(1)
	return nil // unreachable; satisfies the error return signature
}

// buildPhase2ExecArgs constructs the args slice for the second re-exec of
// aide __sandbox-apply (Phase 2: seccomp install + exec inside the PID
// namespace). The slot layout is:
//
//	[0] "aide"                  — argv[0] of the aide re-exec
//	[1] "__sandbox-apply"
//	[2] "--"                    — Phase 2 sentinel
//	[3] agentPath               — absolute binary path for syscall.Exec
//	[4:] agentCmd               — the agent's full argv, including the
//	                              original argv[0] (the name as the user
//	                              invoked it) so the agent sees the same
//	                              argv[0] as it does under
//	                              AllowSubprocess=true.
//
// Passing agentPath and agentCmd[0] independently avoids the previous bug
// where the resolved absolute path overwrote the agent's argv[0], silently
// diverging the two AllowSubprocess paths for any tool that gates on argv[0]
// (busybox multi-call binaries, Node.js process.argv[0], etc.).
func buildPhase2ExecArgs(agentPath string, agentCmd []string) []string {
	out := make([]string, 0, 4+len(agentCmd))
	out = append(out, "aide", "__sandbox-apply", "--", agentPath)
	out = append(out, agentCmd...)
	return out
}

// RunSandboxExec is the __sandbox-exec re-exec handler. It runs inside the
// PID namespace created by forkExecInPIDNamespace, installs the no-subprocess
// seccomp filter, and then execs the agent. The filter must be installed here
// (inside the fork) rather than in the parent so the parent's fork() call is
// not itself blocked by seccomp.
//
// agentPath is the absolute binary to exec; agentCmd is the agent's full
// argv (agentCmd[0] is the name to show in argv[0]). They are independent so
// the kernel loads the right ELF while the program sees its original name.
//
// agentPath is stat'd before seccomp installs so a missing binary returns a
// clean error instead of leaving the process in a seccomp-restricted state
// that cannot exec anything else.
func RunSandboxExec(agentPath string, agentCmd []string) error {
	if agentPath == "" {
		return fmt.Errorf("no agent path")
	}
	if len(agentCmd) == 0 {
		return fmt.Errorf("no agent command")
	}

	if _, err := os.Stat(agentPath); err != nil {
		return fmt.Errorf("agent not found: %w", err)
	}

	if err := installNoSubprocessSeccomp(); err != nil {
		return fmt.Errorf("install seccomp: %w", err)
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

	// Use linuxLandlockGrantedPaths so the profile includes the system-path
	// bootstrap entries (/usr, /lib, /proc, /dev, etc.) that Landlock must
	// allow before any process can exec or do I/O. linuxGrantedPaths only
	// returns the guard-derived set and was giving aide sandbox show an
	// incomplete picture of what the Landlock backend actually enforces.
	gps := linuxLandlockGrantedPaths(policy)
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
		if portPolicy.Mode == "deny_complement" {
			// AllowSet spans the full port range (1-65535 minus denied) — printing
			// every entry would be unreadably verbose. Show only the denied list.
			b.WriteString("## Deny ports:")
			for _, p := range policy.DenyPorts {
				fmt.Fprintf(&b, " %d", p)
			}
			b.WriteString("\n")
		} else {
			b.WriteString("## Allow ports:")
			for _, port := range portPolicy.AllowSet {
				fmt.Fprintf(&b, " %d", port)
			}
			b.WriteString("\n")
		}
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

	// System essentials. Mirror linuxSystemReadable so the bwrap fallback
	// grants the same baseline the Landlock backend grants — TLS bundle,
	// NSS resolution, passwd/group lookup, ld.so cache, /nix/store and
	// Linuxbrew on those hosts. Without this, Go/Node TLS lookups and
	// getpwuid resolution fail under bwrap with no equivalent failure
	// under Landlock. --ro-bind-try silently skips paths absent on this
	// host (e.g. /lib32 on pure-64-bit distros).
	for _, p := range linuxSystemReadable {
		if p == "/proc" {
			continue // bwrap mounts a fresh procfs via --proc below
		}
		bwrapArgs = append(bwrapArgs, "--ro-bind-try", p, p)
	}
	// Mirror linuxSystemWritable. bwrap mount-point flags are heterogeneous
	// per path so this can't be a uniform loop: --dev creates a fresh
	// minimal devtmpfs with the standard nodes (covers /dev and bwrap's
	// auto-handled /dev/pts entries), --tmpfs /dev/shm provides the shared-
	// memory tmpfs that Node V8 heap-snapshot sharing needs (without it
	// shm_open returns ENOENT and Electron-based agents like Claude Code
	// CLI crash on startup), and /run is bind-mounted from the host so the
	// XDG runtime dir, D-Bus session bus socket, and socket-activation
	// sockets stay reachable. --perms 01777 matches the host /dev/shm
	// sticky-bit semantics so anything that probes the mode does not see a
	// surprising value.
	bwrapArgs = append(bwrapArgs,
		"--proc", "/proc",
		"--dev", "/dev",
		"--perms", "01777", "--tmpfs", "/dev/shm",
		"--bind-try", "/run", "/run",
		"--tmpfs", "/tmp",
	)

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

	// Subprocess gate. A seccomp BPF filter blocks clone/fork/vfork at the
	// syscall level — the agent cannot create child processes at all. No
	// --unshare-pid needed: seccomp is the hard enforcement layer, not a
	// namespace-scoped containment.
	if !policy.AllowSubprocess {
		memFile, err := noSubprocessSeccompMemfd()
		if err != nil {
			return fmt.Errorf("seccomp setup: %w", err)
		}
		// Same exec-boundary problem as the Landlock policy fd: cmd.ExtraFiles
		// is honoured only by *exec.Cmd.Start(); the launcher uses raw
		// syscall.Exec to spawn bwrap. The seccomp memfd is created without
		// MFD_CLOEXEC (see noSubprocessSeccompMemfd) so the kernel-allocated
		// fd survives exec; pass that fd number explicitly to bwrap.
		seccompFDNum := int(memFile.Fd())
		cmd.ExtraFiles = []*os.File{memFile}
		bwrapArgs = append(bwrapArgs, "--seccomp", strconv.Itoa(seccompFDNum))
	}

	// Append -- and the original command
	bwrapArgs = append(bwrapArgs, "--")
	bwrapArgs = append(bwrapArgs, cmd.Args...)

	// Rewrite the command
	cmd.Path = bwrapPath
	cmd.Args = append([]string{"bwrap"}, bwrapArgs...)

	if policy.CleanEnv {
		cmd.Env = filterEnv(cmd.Env, policy)
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

// PlatformGrantedPaths returns the complete GrantedPathSet that the Linux
// backend will enforce, including the Landlock system-path bootstrap entries
// (/usr, /lib, /proc, /dev, /tmp, etc.) that are required for any process to
// exec or do I/O. Callers that only need the guard-derived set should use
// DeriveGrantedPathSet instead.
func PlatformGrantedPaths(policy Policy) GrantedPathSet {
	return linuxLandlockGrantedPaths(policy)
}

// filterEnv and expandGlobs are in sandbox.go (shared across platforms).
