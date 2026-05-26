//go:build linux

package sandbox

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt"
	"github.com/jskswamy/aide/pkg/seatbelt/modules"
)

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
	err := RunSandboxExec([]string{"/nonexistent/agent-binary"})
	if err == nil {
		t.Fatal("RunSandboxExec with nonexistent binary should return error")
	}
}

// TestRunSandboxExec_EmptyArgs_ReturnsError confirms the guard for empty arg
// slice.
func TestRunSandboxExec_EmptyArgs_ReturnsError(t *testing.T) {
	err := RunSandboxExec(nil)
	if err == nil {
		t.Fatal("RunSandboxExec with nil args should return error")
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

	// Read the policy JSON that was written
	policyPath := runtimeDir + "/landlock-policy.json"
	policyBytes, err := os.ReadFile(policyPath)
	if err != nil {
		t.Fatalf("reading policy file: %v", err)
	}

	policyJSON := string(policyBytes)

	// Verify the policy JSON contains AllowPorts with 443
	if !strings.Contains(policyJSON, `"AllowPorts":[443]`) {
		t.Errorf("policy JSON should contain AllowPorts=[443], got: %s", policyJSON)
	}

	// Verify the command was rewritten to use aide __sandbox-apply
	args := strings.Join(cmd.Args, " ")
	if !strings.Contains(args, "__sandbox-apply") {
		t.Errorf("expected __sandbox-apply in args, got: %s", args)
	}
	if !strings.Contains(args, policyPath) {
		t.Errorf("expected policy path %s in args, got: %s", policyPath, args)
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

	policyPath := runtimeDir + "/landlock-policy.json"
	policyBytes, err := os.ReadFile(policyPath)
	if err != nil {
		t.Fatalf("reading policy file: %v", err)
	}

	policyJSON := string(policyBytes)
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

	policyPath := runtimeDir + "/landlock-policy.json"
	data, err := os.ReadFile(policyPath)
	if err != nil {
		t.Fatalf("reading policy JSON: %v", err)
	}
	content := string(data)

	// Writable must contain ProjectRoot
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
			portPolicy: PortPolicyEffective{Mode: "allow_only", AllowSet: []int{443}},
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
			portPolicy: PortPolicyEffective{Mode: "allow_only", AllowSet: []int{443, 53}},
			want:       true,
		},
		{
			name:       "outbound with deny-derived allow set still gates",
			mode:       NetworkOutbound,
			portPolicy: PortPolicyEffective{Mode: "deny_complement", AllowSet: []int{80, 443}},
			want:       true,
		},
		{
			name:       "outbound with empty allow set after deny intersection does not gate",
			mode:       NetworkOutbound,
			portPolicy: PortPolicyEffective{Mode: "allow_intersect_deny"},
			want:       false,
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
