//go:build linux

package sandbox

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt"
	"github.com/jskswamy/aide/pkg/seatbelt/modules"
)

// readPolicyFromExtraFiles reads and returns the policy JSON from
// cmd.ExtraFiles[0], which applyLandlock writes to a memfd instead of disk.
func readPolicyFromExtraFiles(t *testing.T, cmd *exec.Cmd) string {
	t.Helper()
	if len(cmd.ExtraFiles) == 0 {
		t.Fatal("cmd.ExtraFiles is empty; expected memfd policy file at index 0")
	}
	data, err := io.ReadAll(cmd.ExtraFiles[0])
	if err != nil {
		t.Fatalf("reading policy from memfd ExtraFiles[0]: %v", err)
	}
	return string(data)
}

// TestWritePolicyToMemfd verifies that writePolicyToMemfd stores the payload in
// anonymous memory (not on the filesystem) and that the data round-trips
// correctly through the returned *os.File.
func TestWritePolicyToMemfd(t *testing.T) {
	payload := []byte(`{"ProjectRoot":"/tmp/project","Network":"outbound"}`)

	f, err := writePolicyToMemfd(payload)
	if err != nil {
		t.Fatalf("writePolicyToMemfd: %v", err)
	}
	defer func() { _ = f.Close() }()

	got, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("ReadAll from memfd: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("memfd round-trip: got %q, want %q", got, payload)
	}
}

func TestLinuxSandbox_NewSandbox_ReturnsLinux(t *testing.T) {
	s := NewSandbox()
	if s == nil {
		t.Fatal("NewSandbox() returned nil")
	}
	if _, ok := s.(*LinuxSandbox); !ok {
		t.Errorf("NewSandbox() returned %T, want *LinuxSandbox", s)
	}
}

func TestLinuxSandbox_LandlockAvailable(t *testing.T) {
	caps := DetectKernelCapabilities()
	t.Logf("Landlock available: %v (ABI %d)", caps.LandlockEnabled, caps.LandlockABI)
}

func TestLinuxSandbox_ApplyBwrap_BasicArgs(t *testing.T) {
	s := &LinuxSandbox{}
	cmd := exec.Command("/usr/bin/echo", "hello")
	policy := Policy{
		ProjectRoot:     "/tmp/project",
		RuntimeDir:      "/tmp/rt",
		TempDir:         "/tmp",
		Network:         NetworkOutbound,
		AllowSubprocess: true,
		CleanEnv:        false,
	}

	bwrapPath, err := exec.LookPath("bwrap")
	if err != nil {
		t.Skip("bwrap not on PATH")
	}

	err = s.applyBwrap(cmd, policy, bwrapPath)
	if err != nil {
		t.Fatalf("applyBwrap error: %v", err)
	}

	if cmd.Path != bwrapPath {
		t.Errorf("cmd.Path = %q, want %q", cmd.Path, bwrapPath)
	}

	args := strings.Join(cmd.Args, " ")

	// Check writable bind (--bind-try to tolerate module-declared paths that may not exist yet)
	if !strings.Contains(args, "--bind-try /tmp/project /tmp/project") {
		t.Errorf("missing --bind-try for writable path in: %s", args)
	}

	// Check original command is after --
	if !strings.Contains(args, "-- /usr/bin/echo hello") {
		t.Errorf("original command not after -- in: %s", args)
	}
}

// TestLinuxSandbox_ApplyBwrap_MirrorsLinuxSystemReadable pins the fix for the
// fallback divergence: the Landlock backend grants every path in
// linuxSystemReadable, and the bwrap fallback must mirror them, otherwise
// TLS/NSS/passwd lookups would fail under bwrap with no equivalent failure
// under Landlock. /proc is excluded because bwrap mounts a fresh procfs via
// --proc, not --ro-bind.
func TestLinuxSandbox_ApplyBwrap_MirrorsLinuxSystemReadable(t *testing.T) {
	s := &LinuxSandbox{}
	cmd := exec.Command("/usr/bin/echo")
	policy := Policy{
		Network:         NetworkOutbound,
		AllowSubprocess: true,
	}

	bwrapPath, err := exec.LookPath("bwrap")
	if err != nil {
		t.Skip("bwrap not on PATH")
	}
	if err := s.applyBwrap(cmd, policy, bwrapPath); err != nil {
		t.Fatalf("applyBwrap error: %v", err)
	}

	args := strings.Join(cmd.Args, " ")
	for _, p := range linuxSystemReadable {
		if p == "/proc" {
			continue
		}
		want := "--ro-bind-try " + p + " " + p
		if !strings.Contains(args, want) {
			t.Errorf("bwrap fallback omits %q from system readables; missing %q in: %s", p, want, args)
		}
	}
	for _, want := range []string{"--proc /proc", "--dev /dev", "--tmpfs /tmp"} {
		if !strings.Contains(args, want) {
			t.Errorf("bwrap mount-point flag %q missing in: %s", want, args)
		}
	}
	// linuxSystemWritable contains /dev/shm and /run, both critical to
	// runtime parity with Landlock:
	//   - /dev/shm tmpfs: V8 heap-snapshot sharing (shm_open) crashes
	//     Electron/Node agents with ENOENT under bwrap when absent.
	//   - /run bind: D-Bus session bus socket, XDG_RUNTIME_DIR, socket
	//     activation. Silent failure under bwrap with no equivalent under
	//     Landlock.
	for _, want := range []string{"--tmpfs /dev/shm", "--bind-try /run /run"} {
		if !strings.Contains(args, want) {
			t.Errorf("bwrap fallback omits writable system mount %q (mirrors linuxSystemWritable); args: %s", want, args)
		}
	}
}

func TestLinuxSandbox_ApplyBwrap_NetworkNone(t *testing.T) {
	s := &LinuxSandbox{}
	cmd := exec.Command("/usr/bin/echo")
	policy := Policy{
		Network:         NetworkNone,
		AllowSubprocess: true,
	}

	bwrapPath, err := exec.LookPath("bwrap")
	if err != nil {
		t.Skip("bwrap not on PATH")
	}

	err = s.applyBwrap(cmd, policy, bwrapPath)
	if err != nil {
		t.Fatalf("applyBwrap error: %v", err)
	}

	args := strings.Join(cmd.Args, " ")
	if !strings.Contains(args, "--unshare-net") {
		t.Errorf("expected --unshare-net for NetworkNone, got: %s", args)
	}
}

func TestLinuxSandbox_ApplyBwrap_NoSubprocess(t *testing.T) {
	s := &LinuxSandbox{}
	cmd := exec.Command("/usr/bin/echo")
	policy := Policy{
		AllowSubprocess: false,
		Network:         NetworkOutbound,
	}

	bwrapPath, err := exec.LookPath("bwrap")
	if err != nil {
		t.Skip("bwrap not on PATH")
	}

	err = s.applyBwrap(cmd, policy, bwrapPath)
	if err != nil {
		t.Fatalf("applyBwrap error: %v", err)
	}

	args := strings.Join(cmd.Args, " ")
	// Subprocess enforcement is via seccomp (--seccomp <fd>), not --unshare-pid.
	if strings.Contains(args, "--unshare-pid") {
		t.Errorf("--unshare-pid should not be present (seccomp is the enforcement mechanism), got: %s", args)
	}
	if !strings.Contains(args, "--seccomp") {
		t.Errorf("expected --seccomp when AllowSubprocess=false, got: %s", args)
	}
}

func TestLinuxSandbox_ApplyBwrap_CleanEnv(t *testing.T) {
	s := &LinuxSandbox{}
	cmd := exec.Command("/usr/bin/echo")
	cmd.Env = []string{
		"PATH=/usr/bin",
		"HOME=/root",
		"SECRET_KEY=leaked",
		"TERM=xterm",
	}
	policy := Policy{
		CleanEnv:        true,
		Network:         NetworkOutbound,
		AllowSubprocess: true,
	}

	bwrapPath, err := exec.LookPath("bwrap")
	if err != nil {
		t.Skip("bwrap not on PATH")
	}

	err = s.applyBwrap(cmd, policy, bwrapPath)
	if err != nil {
		t.Fatalf("applyBwrap error: %v", err)
	}

	for _, e := range cmd.Env {
		if strings.HasPrefix(e, "SECRET_KEY=") {
			t.Errorf("SECRET_KEY should have been filtered out, got: %s", e)
		}
	}

	found := false
	for _, e := range cmd.Env {
		if strings.HasPrefix(e, "PATH=") {
			found = true
		}
	}
	if !found {
		t.Error("PATH should be kept in clean env")
	}
}

func TestLinuxSandbox_Apply_FallsBackGracefully(t *testing.T) {
	// When neither Landlock nor bwrap is available, Apply should not error
	s := &LinuxSandbox{}
	cmd := exec.Command("/usr/bin/echo", "test")
	runtimeDir := t.TempDir()
	policy := DefaultPolicy(Paths{ProjectRoot: "/tmp/proj", RuntimeDir: runtimeDir, TempDir: "/tmp"}, nil)

	// This test exercises the full Apply path.
	// On systems with bwrap (our devcontainer), it will use bwrap.
	// On systems with Landlock, it will use Landlock.
	// Either way, it should not error.
	err := s.Apply(cmd, policy, runtimeDir)
	if err != nil {
		t.Fatalf("Apply should not error: %v", err)
	}
}

// TestApplyLandlock_NoSubprocess_NoCLONE_NEWPID pins that applyLandlock no
// longer sets SysProcAttr.Cloneflags to CLONE_NEWPID. The flag was dead code
// (syscall.Exec ignores SysProcAttr); real PID namespace isolation is now done
// inside RunSandboxApply via forkExecInPIDNamespace, which calls ForkExec with
// CLONE_NEWPID after Landlock is applied in the child.
func TestApplyLandlock_NoSubprocess_NoCLONE_NEWPID(t *testing.T) {
	s := &LinuxSandbox{}
	cmd := exec.Command("/usr/bin/echo", "hi")
	runtimeDir := t.TempDir()
	policy := Policy{
		ProjectRoot:     runtimeDir,
		RuntimeDir:      runtimeDir,
		Network:         NetworkOutbound,
		AllowSubprocess: false,
	}

	if err := s.applyLandlock(cmd, policy, runtimeDir); err != nil {
		t.Fatalf("applyLandlock: %v", err)
	}

	if cmd.SysProcAttr != nil && (cmd.SysProcAttr.Cloneflags&syscall.CLONE_NEWPID) != 0 {
		t.Errorf("applyLandlock must not set CLONE_NEWPID on SysProcAttr (syscall.Exec ignores it; PID namespace is created by forkExecInPIDNamespace inside RunSandboxApply)")
	}
}

// TestRunSandboxExec_MissingAgent_ReturnsError confirms RunSandboxExec fails
// cleanly when given a non-existent agent binary rather than panicking or
// silently succeeding.
func TestRunSandboxExec_MissingAgent_ReturnsError(t *testing.T) {
	err := RunSandboxExec("/nonexistent/agent-binary", []string{"agent-binary"})
	if err == nil {
		t.Fatal("RunSandboxExec with nonexistent binary should return error")
	}
}

func TestRunSandboxExec_EmptyArgs_ReturnsError(t *testing.T) {
	err := RunSandboxExec("/some/path", nil)
	if err == nil {
		t.Fatal("RunSandboxExec with empty argv should return error")
	}
}

func TestRunSandboxExec_EmptyPath_ReturnsError(t *testing.T) {
	err := RunSandboxExec("", []string{"agent"})
	if err == nil {
		t.Fatal("RunSandboxExec with empty agentPath should return error")
	}
}

// TestBuildPhase2ExecArgs_PreservesOriginalArgv0 pins the Greptile P2 fix:
// the resolved absolute agentPath must occupy slot 3 (so syscall.Exec inside
// RunSandboxExec loads the right binary), but the agent's original argv[0]
// (the name as the user invoked it) must be preserved at slot 4 so the
// exec'd agent sees the same argv[0] under AllowSubprocess=false as it
// does under AllowSubprocess=true. Previously the slice was
// ["aide", "__sandbox-apply", "--", agentPath, agentCmd[1:]...] — the
// original argv[0] was dropped and the resolved path leaked into argv[0].
func TestBuildPhase2ExecArgs_PreservesOriginalArgv0(t *testing.T) {
	agentPath := "/usr/local/bin/claude"
	agentCmd := []string{"claude", "--print", "hello"}

	got := buildPhase2ExecArgs(agentPath, agentCmd)

	want := []string{"aide", "__sandbox-apply", "--", "/usr/local/bin/claude", "claude", "--print", "hello"}
	if len(got) != len(want) {
		t.Fatalf("buildPhase2ExecArgs len = %d, want %d; got %v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("buildPhase2ExecArgs[%d] = %q, want %q (full: %v)", i, got[i], w, got)
		}
	}
	if got[3] == got[4] && agentPath != agentCmd[0] {
		t.Errorf("resolved path leaked into argv[0]: slot 3 = slot 4 = %q (must differ when name != path)", got[3])
	}
}

func TestLandlock_PortFiltering_RewritesCmd(t *testing.T) {
	s := &LinuxSandbox{}
	cmd := exec.Command("/usr/bin/echo", "hello")
	policy := Policy{
		ProjectRoot:     "/tmp/project",
		RuntimeDir:      "/tmp/rt",
		TempDir:         "/tmp",
		Network:         NetworkOutbound,
		AllowPorts:      []int{443},
		AllowSubprocess: true,
	}

	runtimeDir := t.TempDir()
	err := s.applyLandlock(cmd, policy, runtimeDir)
	if err != nil {
		t.Fatalf("applyLandlock error: %v", err)
	}

	policyJSON := readPolicyFromExtraFiles(t, cmd)

	if !strings.Contains(policyJSON, `"AllowPorts":[443]`) {
		t.Errorf("policy JSON should contain AllowPorts=[443], got: %s", policyJSON)
	}

	args := strings.Join(cmd.Args, " ")
	if !strings.Contains(args, "__sandbox-apply") {
		t.Errorf("expected __sandbox-apply in args, got: %s", args)
	}
	if !strings.Contains(args, "--policy-fd=") {
		t.Errorf("expected --policy-fd= in args, got: %s", args)
	}
}

func TestLandlock_PortFiltering_DenyPorts(t *testing.T) {
	s := &LinuxSandbox{}
	cmd := exec.Command("/usr/bin/echo", "hello")
	policy := Policy{
		ProjectRoot:     "/tmp/project",
		RuntimeDir:      "/tmp/rt",
		TempDir:         "/tmp",
		Network:         NetworkOutbound,
		DenyPorts:       []int{22, 25},
		AllowSubprocess: true,
	}

	runtimeDir := t.TempDir()
	err := s.applyLandlock(cmd, policy, runtimeDir)
	if err != nil {
		t.Fatalf("applyLandlock error: %v", err)
	}

	policyJSON := readPolicyFromExtraFiles(t, cmd)
	if !strings.Contains(policyJSON, `"DenyPorts":[22,25]`) {
		t.Errorf("policy JSON should contain DenyPorts=[22,25], got: %s", policyJSON)
	}
}

func TestBwrap_PortFiltering_Warning(t *testing.T) {
	s := &LinuxSandbox{}
	cmd := exec.Command("/usr/bin/echo", "hello")
	policy := Policy{
		ProjectRoot:     "/tmp/project",
		RuntimeDir:      "/tmp/rt",
		TempDir:         "/tmp",
		Network:         NetworkOutbound,
		AllowPorts:      []int{443},
		AllowSubprocess: true,
	}

	bwrapPath, err := exec.LookPath("bwrap")
	if err != nil {
		t.Skip("bwrap not on PATH")
	}

	// Redirect os.Stderr to capture the always-on warning.
	r, w, _ := os.Pipe()
	origStderr := os.Stderr
	os.Stderr = w

	err = s.applyBwrap(cmd, policy, bwrapPath)

	_ = w.Close()
	os.Stderr = origStderr

	var stderrBuf bytes.Buffer
	_, _ = stderrBuf.ReadFrom(r)

	if err != nil {
		t.Fatalf("applyBwrap error: %v", err)
	}

	stderrOutput := stderrBuf.String()
	if !strings.Contains(stderrOutput, "Port-level filtering not supported by bwrap") {
		t.Errorf("expected warning about port filtering on stderr, got: %q", stderrOutput)
	}

	// Verify execution still proceeds (cmd.Path should be bwrap)
	if cmd.Path != bwrapPath {
		t.Errorf("cmd.Path = %q, want %q (execution should proceed despite warning)", cmd.Path, bwrapPath)
	}
}

// TestGenerateProfile_ContainsHeaderLines verifies the isolation tier/backend header is present in the generated profile.
func TestGenerateProfile_ContainsHeaderLines(t *testing.T) {
	s := &LinuxSandbox{}
	runtimeDir := t.TempDir()
	policy := Policy{
		ProjectRoot:     runtimeDir,
		RuntimeDir:      runtimeDir,
		TempDir:         "/tmp",
		Network:         NetworkOutbound,
		AllowPorts:      []int{443, 80},
		AllowSubprocess: true,
		Guards:          []string{},
	}

	profile, err := s.GenerateProfile(policy)
	if err != nil {
		t.Fatalf("GenerateProfile error: %v", err)
	}

	requiredLines := []string{
		"# Tier:", "# Backend:", "# Port filtering:",
		"## Writable paths", "## Network:",
	}
	for _, line := range requiredLines {
		if !strings.Contains(profile, line) {
			t.Errorf("GenerateProfile missing %q in:\n%s", line, profile)
		}
	}
}

// TestGenerateProfile_IncludesSystemPaths verifies that GenerateProfile uses
// linuxLandlockGrantedPaths rather than linuxGrantedPaths, so the profile
// includes the system-path bootstrap entries that Landlock actually enforces
// (/usr, /proc, /dev, etc.). Previously it used linuxGrantedPaths which
// returned only guard-derived paths, making "aide sandbox show" omit the
// majority of the allow-list.
func TestGenerateProfile_IncludesSystemPaths(t *testing.T) {
	s := &LinuxSandbox{}
	runtimeDir := t.TempDir()
	policy := Policy{
		ProjectRoot:     runtimeDir,
		RuntimeDir:      runtimeDir,
		TempDir:         "/tmp",
		Network:         NetworkOutbound,
		AllowSubprocess: true,
		Guards:          []string{},
	}

	profile, err := s.GenerateProfile(policy)
	if err != nil {
		t.Fatalf("GenerateProfile error: %v", err)
	}

	// /usr is always present in linuxSystemReadable and exists on every Linux
	// host; if it's missing from the profile, GenerateProfile is using the
	// incomplete linuxGrantedPaths instead of linuxLandlockGrantedPaths.
	if !strings.Contains(profile, "/usr") {
		t.Errorf("GenerateProfile omits /usr — profile is incomplete; system paths are missing:\n%s", profile)
	}
}

// TestApply_Unavailable_DoesNotMutateCmd verifies that when no sandbox is available,
// Apply returns nil, does not mutate cmd, and LastTier().Tier == "unavailable".
func TestApply_Unavailable_DoesNotMutateCmd(t *testing.T) {
	caps := DetectKernelCapabilities()
	if caps.LandlockEnabled || caps.BwrapAvailable {
		t.Skip("skipping: sandbox backends available on this host; cannot reach unavailable path")
	}

	s := &LinuxSandbox{}
	cmd := exec.Command("/usr/bin/echo", "hi")
	originalPath := cmd.Path
	originalArgs := strings.Join(cmd.Args, " ")
	runtimeDir := t.TempDir()

	policy := Policy{
		ProjectRoot: runtimeDir,
		RuntimeDir:  runtimeDir,
		Network:     NetworkOutbound,
	}

	// Arrange
	err := s.Apply(cmd, policy, runtimeDir)

	// Assert
	if err != nil {
		t.Errorf("Apply returned unexpected error: %v", err)
	}
	if cmd.Path != originalPath {
		t.Errorf("cmd.Path mutated: got %q, want %q", cmd.Path, originalPath)
	}
	if strings.Join(cmd.Args, " ") != originalArgs {
		t.Errorf("cmd.Args mutated: got %q, want %q", strings.Join(cmd.Args, " "), originalArgs)
	}
	if s.LastTier() == nil || s.LastTier().Tier != TierUnavailable {
		t.Errorf("LastTier = %v, want unavailable", s.LastTier())
	}
}

// TestApply_LandlockDegradedNetworkNone_PrefersBwrap is an integration regression
// guard for the P1 reported on PR #2 (Greptile): on hosts with Landlock present
// but ABI < 4 AND bubblewrap installed, a policy of `network: none` must NOT
// hard-error out of Apply. The tier resolver picks bwrap (which can enforce
// network=none via --unshare-net) and Apply must honour that selection.
//
// Skips unless the host shape actually matches (Landlock ABI < 4 + bwrap on
// PATH). Real exercise happens on CI's ubuntu-22.04 matrix entry.
func TestApply_LandlockDegradedNetworkNone_PrefersBwrap(t *testing.T) {
	caps := DetectKernelCapabilities()
	if !caps.LandlockEnabled || caps.LandlockABI >= 4 || !caps.BwrapAvailable {
		t.Skipf("skipping: requires Landlock ABI < 4 + bwrap on PATH; got landlock=%v abi=%d bwrap=%v",
			caps.LandlockEnabled, caps.LandlockABI, caps.BwrapAvailable)
	}
	bwrapPath, err := exec.LookPath("bwrap")
	if err != nil {
		t.Skipf("skipping: bwrap not on PATH: %v", err)
	}

	s := &LinuxSandbox{}
	cmd := exec.Command("/usr/bin/echo", "hi")
	runtimeDir := t.TempDir()
	policy := Policy{
		ProjectRoot: runtimeDir,
		RuntimeDir:  runtimeDir,
		Network:     NetworkNone,
	}

	if applyErr := s.Apply(cmd, policy, runtimeDir); applyErr != nil {
		t.Fatalf("Apply returned error: %v (regression: degraded Landlock + bwrap-supported policy must fall back, not error)", applyErr)
	}
	tier := s.LastTier()
	if tier == nil || tier.Backend != BackendBwrap {
		t.Fatalf("LastTier = %+v, want Backend=%q", tier, BackendBwrap)
	}
	if cmd.Path != bwrapPath {
		t.Errorf("cmd.Path = %q, want %q (Apply did not dispatch to bwrap)", cmd.Path, bwrapPath)
	}
}

// TestApply_BwrapWithPortRules_DegradedTier verifies that when bwrap is the fallback
// and port rules are configured, the tier is degraded with PortFiltering=degraded.
func TestApply_BwrapWithPortRules_DegradedTier(t *testing.T) {
	caps := KernelCapabilities{LandlockEnabled: false, BwrapAvailable: true}
	policy := Policy{AllowPorts: []int{443}}
	tier := ComputeIsolationTier(caps, policy)

	if tier.Tier != TierDegraded {
		t.Errorf("Tier = %q, want degraded", tier.Tier)
	}
	if tier.PortFiltering != PortFilteringDegraded {
		t.Errorf("PortFiltering = %q, want degraded", tier.PortFiltering)
	}
	if !strings.Contains(tier.Reason, "bwrap fallback") {
		t.Errorf("Reason %q should mention bwrap fallback", tier.Reason)
	}
}

// TestApply_PolicyJSON_UsesGrantedPathSet verifies Apply writes a policy JSON whose
// path fields are driven by DeriveGrantedPathSet (no coarse $HOME).
func TestApply_PolicyJSON_UsesGrantedPathSet(t *testing.T) {
	runtimeDir := t.TempDir()
	projectRoot := t.TempDir()

	policy := Policy{
		ProjectRoot:     projectRoot,
		RuntimeDir:      runtimeDir,
		TempDir:         "/tmp",
		Network:         NetworkOutbound,
		AllowSubprocess: true,
		Guards:          []string{}, // empty — no guard paths
	}

	s := &LinuxSandbox{}
	cmd := exec.Command("/usr/bin/echo", "hi")

	if err := s.applyLandlock(cmd, policy, runtimeDir); err != nil {
		t.Fatalf("applyLandlock error: %v", err)
	}

	content := readPolicyFromExtraFiles(t, cmd)

	if !strings.Contains(content, projectRoot) {
		t.Errorf("policy JSON should contain ProjectRoot %q, got: %s", projectRoot, content)
	}

	// Home directory should NOT be present as a plain writable path
	// (it was only added before as a coarse allow; GrantedPathSet replaces this).
	home, _ := os.UserHomeDir()
	if strings.Contains(content, `"`+home+`"`) {
		t.Errorf("policy JSON must not contain plain $HOME %q as writable; use guard-derived paths instead", home)
	}
}

// TestRunSandboxApply_InvalidPort_ReturnsError verifies port validation rejects out-of-range values.
func TestRunSandboxApply_InvalidPort_ReturnsError(t *testing.T) {
	if err := ValidatePortRange([]int{99999}); err == nil {
		t.Error("expected error for port 99999")
	}
}

func TestShouldGateNetwork(t *testing.T) {
	tests := []struct {
		name       string
		mode       NetworkMode
		portPolicy PortPolicyEffective
		want       bool
	}{
		{
			name:       "none mode always gates (deny all TCP)",
			mode:       NetworkNone,
			portPolicy: PortPolicyEffective{Mode: "unrestricted"},
			want:       true,
		},
		{
			name:       "unrestricted mode never gates",
			mode:       NetworkUnrestricted,
			portPolicy: PortPolicyEffective{Mode: "unrestricted"},
			want:       false,
		},
		{
			name:       "unrestricted mode never gates even with stale port rules",
			mode:       NetworkUnrestricted,
			portPolicy: PortPolicyEffective{Mode: "allow_only", AllowSet: []uint16{443}},
			want:       false,
		},
		{
			name:       "outbound with no port rules does not gate (regression: fixes silent TCP block)",
			mode:       NetworkOutbound,
			portPolicy: PortPolicyEffective{Mode: "unrestricted"},
			want:       false,
		},
		{
			name:       "outbound with allow ports gates and applies allow-set",
			mode:       NetworkOutbound,
			portPolicy: PortPolicyEffective{Mode: "allow_only", AllowSet: []uint16{443, 53}},
			want:       true,
		},
		{
			name:       "outbound with deny-derived allow set still gates",
			mode:       NetworkOutbound,
			portPolicy: PortPolicyEffective{Mode: "deny_complement", AllowSet: []uint16{80, 443}},
			want:       true,
		},
		{
			name:       "outbound with allow_intersect_deny empty set gates (no ports allowed)",
			mode:       NetworkOutbound,
			portPolicy: PortPolicyEffective{Mode: "allow_intersect_deny"},
			want:       true,
		},
		{
			// Regression: DenyPorts covering 1-65535 → Mode=deny_complement,
			// AllowSet empty. Previous len(AllowSet) gate let everything
			// through; the user-expressed "block all ports" intent must
			// produce "no outbound TCP" instead.
			name:       "outbound with deny-all-ports gates (deny_complement, empty AllowSet)",
			mode:       NetworkOutbound,
			portPolicy: PortPolicyEffective{Mode: "deny_complement"},
			want:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldGateNetwork(tt.mode, tt.portPolicy)
			if got != tt.want {
				t.Errorf("shouldGateNetwork(%q, %+v) = %v, want %v", tt.mode, tt.portPolicy, got, tt.want)
			}
		})
	}
}

func TestFilterEnv(t *testing.T) {
	env := []string{
		"PATH=/usr/bin",
		"HOME=/home/user",
		"SECRET_API_KEY=sk-123",
		"TERM=xterm-256color",
		"ANTHROPIC_API_KEY=sk-ant-123",
		"XDG_CONFIG_HOME=/home/user/.config",
	}

	filtered := filterEnv(env, Policy{})

	kept := make(map[string]bool)
	for _, e := range filtered {
		key := strings.SplitN(e, "=", 2)[0]
		kept[key] = true
	}

	if !kept["PATH"] {
		t.Error("PATH should be kept")
	}
	if !kept["HOME"] {
		t.Error("HOME should be kept")
	}
	if !kept["TERM"] {
		t.Error("TERM should be kept")
	}
	if !kept["XDG_CONFIG_HOME"] {
		t.Error("XDG_CONFIG_HOME should be kept")
	}
	if kept["SECRET_API_KEY"] {
		t.Error("SECRET_API_KEY should be filtered out")
	}
	if kept["ANTHROPIC_API_KEY"] {
		t.Error("ANTHROPIC_API_KEY should be filtered out")
	}
}

// fakeEnvProviderModule lets the test assert filterEnv preserves the keys an
// AgentModule declares via the seatbelt.EnvProvider interface, without
// depending on a specific real module's key surface.
type fakeEnvProviderModule struct {
	name string
	keys []string
}

func (f *fakeEnvProviderModule) Name() string { return f.name }

func (f *fakeEnvProviderModule) Rules(_ *seatbelt.Context) seatbelt.GuardResult {
	return seatbelt.GuardResult{}
}

func (f *fakeEnvProviderModule) AgentEnv(_ *seatbelt.Context) map[string]string {
	out := make(map[string]string, len(f.keys))
	for _, k := range f.keys {
		out[k] = "value-set-by-module"
	}
	return out
}

func TestFilterEnv_PreservesAgentModuleEnvKeys(t *testing.T) {
	policy := Policy{
		AgentModule: &fakeEnvProviderModule{
			name: "fake-agent",
			keys: []string{"CLAUDE_CONFIG_DIR", "FAKE_AGENT_STATE"},
		},
	}
	env := []string{
		"PATH=/usr/bin",
		"CLAUDE_CONFIG_DIR=/run/aide-1234/claude",
		"FAKE_AGENT_STATE=/run/aide-1234/fake",
		"SECRET_API_KEY=sk-123",
	}

	filtered := filterEnv(env, policy)

	got := make(map[string]string)
	for _, e := range filtered {
		k, v, _ := strings.Cut(e, "=")
		got[k] = v
	}

	if got["CLAUDE_CONFIG_DIR"] != "/run/aide-1234/claude" {
		t.Errorf("CLAUDE_CONFIG_DIR should survive CleanEnv strip when AgentModule injects it; got %q", got["CLAUDE_CONFIG_DIR"])
	}
	if got["FAKE_AGENT_STATE"] != "/run/aide-1234/fake" {
		t.Errorf("FAKE_AGENT_STATE should survive CleanEnv strip when AgentModule injects it; got %q", got["FAKE_AGENT_STATE"])
	}
	if _, present := got["SECRET_API_KEY"]; present {
		t.Errorf("SECRET_API_KEY should still be stripped; got %q", got["SECRET_API_KEY"])
	}
}

// nonEnvProviderModule covers the path where the AgentModule does not
// implement seatbelt.EnvProvider — filterEnv must fall back to the bare
// essentials list and not panic on the type assertion.
type nonEnvProviderModule struct{}

func (n *nonEnvProviderModule) Name() string { return "non-env" }
func (n *nonEnvProviderModule) Rules(_ *seatbelt.Context) seatbelt.GuardResult {
	return seatbelt.GuardResult{}
}

// recordingAgentModule captures the seatbelt.Context handed to Rules so
// tests can assert that policyToJSON threads homeDir correctly to the
// AgentModule (rather than silently passing "").
type recordingAgentModule struct {
	gotContext *seatbelt.Context
	writable   []string
}

func (r *recordingAgentModule) Name() string { return "recording-agent" }
func (r *recordingAgentModule) Rules(ctx *seatbelt.Context) seatbelt.GuardResult {
	r.gotContext = ctx
	return seatbelt.GuardResult{Writable: r.writable}
}

// TestPolicyToJSON_AgentModule_ThreadsHomeDir pins the contract that policyToJSON
// resolves $HOME and passes it through to AgentModule.Rules. Previously the
// os.UserHomeDir() error was silently discarded; the recording module now
// asserts the context it actually received.
func TestPolicyToJSON_AgentModule_ThreadsHomeDir(t *testing.T) {
	wantHome, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("os.UserHomeDir not available: %v", err)
	}
	mod := &recordingAgentModule{writable: []string{"/agent/state"}}
	policy := Policy{AgentModule: mod, HomeDir: wantHome}

	got, err := policyToJSON(policy)
	if err != nil {
		t.Fatalf("policyToJSON returned error: %v", err)
	}

	if mod.gotContext == nil {
		t.Fatal("AgentModule.Rules was not called")
	}
	if mod.gotContext.HomeDir != wantHome {
		t.Errorf("AgentModule received HomeDir=%q, want %q", mod.gotContext.HomeDir, wantHome)
	}
	if len(got.AgentWritable) != 1 || got.AgentWritable[0] != "/agent/state" {
		t.Errorf("AgentWritable = %v, want [/agent/state]", got.AgentWritable)
	}
}

// TestPolicyToJSON_NoAgentModule_NoError verifies the AgentModule==nil
// short-circuit returns cleanly without touching os.UserHomeDir.
func TestPolicyToJSON_NoAgentModule_NoError(t *testing.T) {
	policy := Policy{
		ProjectRoot: "/tmp/proj",
		RuntimeDir:  "/tmp/rt",
		Network:     NetworkOutbound,
	}
	got, err := policyToJSON(policy)
	if err != nil {
		t.Fatalf("policyToJSON returned error: %v", err)
	}
	if got.AgentWritable != nil {
		t.Errorf("AgentWritable = %v, want nil", got.AgentWritable)
	}
	if got.ProjectRoot != "/tmp/proj" {
		t.Errorf("ProjectRoot = %q, want /tmp/proj", got.ProjectRoot)
	}
}

// TestPolicyToJSON_EnvRoundtrip verifies that policy.Env is preserved through
// policyToJSON → policyFromJSON so guard env-var lookups (KUBECONFIG,
// AWS_CONFIG_FILE, etc.) in the re-exec child match the parent evaluation.
func TestPolicyToJSON_EnvRoundtrip(t *testing.T) {
	env := []string{
		"KUBECONFIG=/opt/kube/config",
		"AWS_CONFIG_FILE=/opt/aws/config",
		"PATH=/usr/bin",
	}
	policy := Policy{
		ProjectRoot: "/tmp/proj",
		Env:         env,
	}

	j, err := policyToJSON(policy)
	if err != nil {
		t.Fatalf("policyToJSON: %v", err)
	}
	if len(j.Env) != len(env) {
		t.Fatalf("Env length after policyToJSON: got %d, want %d", len(j.Env), len(env))
	}

	restored := policyFromJSON(j)
	if len(restored.Env) != len(env) {
		t.Fatalf("Env length after policyFromJSON: got %d, want %d", len(restored.Env), len(env))
	}
	for i, e := range env {
		if restored.Env[i] != e {
			t.Errorf("Env[%d]: got %q, want %q", i, restored.Env[i], e)
		}
	}
}

func TestFilterEnv_AgentModuleWithoutEnvProvider(t *testing.T) {
	policy := Policy{AgentModule: &nonEnvProviderModule{}}
	env := []string{
		"PATH=/usr/bin",
		"CLAUDE_CONFIG_DIR=/should/be/stripped",
	}

	filtered := filterEnv(env, policy)

	for _, e := range filtered {
		if strings.HasPrefix(e, "CLAUDE_CONFIG_DIR=") {
			t.Errorf("CLAUDE_CONFIG_DIR must not be preserved when no EnvProvider is in play: %s", e)
		}
	}
}

// TestLinuxLandlockGrantedPaths_DenyWinsForSymlinkedSystemPath verifies that
// linuxLandlockGrantedPaths uses symlink-resolved paths for system entries so
// that deny-wins works correctly on merged-usr Linux (where /bin → /usr/bin).
// If a symlink target is denied, the unresolved name must also be excluded.
func TestLinuxLandlockGrantedPaths_DenyWinsForSymlinkedSystemPath(t *testing.T) {
	// Arrange: create a directory that will act as the "real" system path, and
	// a symlink pointing to it (simulating the merged-usr /bin → /usr/bin pattern).
	realDir := t.TempDir()
	symlinkDir := t.TempDir() + "/symlink-bin"
	if err := os.Symlink(realDir, symlinkDir); err != nil {
		t.Skip("cannot create symlink:", err)
	}
	// Deny the real (resolved) target. linuxLandlockGrantedPaths should resolve
	// the symlink before checking deny-wins, so the symlink itself is also excluded.
	policy := Policy{
		ExtraDenied: []string{realDir},
		Guards:      []string{},
	}
	gps := linuxLandlockGrantedPaths(policy)

	// The symlink itself must not appear in Readable (it resolves to a denied dir).
	for _, p := range gps.Readable {
		if p == symlinkDir {
			t.Errorf("symlink %q pointing to denied %q must not appear in Readable", symlinkDir, realDir)
		}
		if p == realDir {
			t.Errorf("denied dir %q must not appear in Readable", realDir)
		}
	}
}

// TestFilterEnv_RealClaudeModule_PreservesClaudeConfigDir is the end-to-end
// regression guard for the Linux Claude agent: launcher injects
// CLAUDE_CONFIG_DIR via applyAgentEnv, then sandbox.Apply calls filterEnv,
// which must keep the key alive. If this test fails, Claude's state writes
// land in $HOME/.claude* and get denied by Landlock at runtime.
func TestFilterEnv_RealClaudeModule_PreservesClaudeConfigDir(t *testing.T) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("os.UserHomeDir not available: %v", err)
	}
	policy := Policy{
		AgentModule: modules.ClaudeAgent(),
		ProjectRoot: "/proj",
		TempDir:     "/tmp",
		HomeDir:     homeDir,
	}
	env := []string{
		"PATH=/usr/bin",
		"CLAUDE_CONFIG_DIR=/run/aide-9/claude",
		"ANTHROPIC_API_KEY=sk-leak",
	}

	filtered := filterEnv(env, policy)

	var sawClaude, sawSecret bool
	for _, e := range filtered {
		switch strings.SplitN(e, "=", 2)[0] {
		case "CLAUDE_CONFIG_DIR":
			sawClaude = true
		case "ANTHROPIC_API_KEY":
			sawSecret = true
		}
	}
	if !sawClaude {
		t.Errorf("CLAUDE_CONFIG_DIR was stripped by CleanEnv; filtered=%v", filtered)
	}
	if sawSecret {
		t.Errorf("ANTHROPIC_API_KEY should still be stripped; filtered=%v", filtered)
	}
}

// TestLinuxSystemReadable_SysfsScope verifies that the sysfs grant is narrowed
// to /sys/fs/cgroup rather than the full /sys tree. Granting the whole tree
// exposes /sys/class/net, /sys/bus/usb, /sys/kernel/security, and debugfs to
// the sandboxed agent. Bun/Node only need cgroup membership information, so
// the allow-list is scoped to /sys/fs/cgroup.
func TestLinuxSystemReadable_SysfsScope(t *testing.T) {
	broadSysfs := "/sys"
	narrowCgroup := "/sys/fs/cgroup"

	var hasBroad, hasNarrow bool
	for _, p := range linuxSystemReadable {
		if p == broadSysfs {
			hasBroad = true
		}
		if p == narrowCgroup {
			hasNarrow = true
		}
	}

	if hasBroad {
		t.Errorf("linuxSystemReadable must not contain %q: exposes /sys/class/net, /sys/bus/usb, /sys/kernel/security, and debugfs", broadSysfs)
	}
	if !hasNarrow {
		t.Errorf("linuxSystemReadable must contain %q for Bun/Node cgroup queries", narrowCgroup)
	}
}
