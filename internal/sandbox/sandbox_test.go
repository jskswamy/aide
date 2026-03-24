package sandbox

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt"
	"github.com/jskswamy/aide/pkg/seatbelt/guards"
)

func TestDefaultPolicy_GuardNames(t *testing.T) {
	policy := DefaultPolicy("/tmp/proj", "/tmp/rt", "/tmp", nil)

	expected := guards.DefaultGuardNames()
	if len(policy.Guards) != len(expected) {
		t.Fatalf("expected %d guards, got %d: %v", len(expected), len(policy.Guards), policy.Guards)
	}
	for i, name := range expected {
		if policy.Guards[i] != name {
			t.Errorf("Guards[%d]: expected %q, got %q", i, name, policy.Guards[i])
		}
	}
}

func TestDefaultPolicy_Paths(t *testing.T) {
	projectRoot := "/tmp/myproject"
	runtimeDir := "/tmp/aide-12345"
	tempDir := "/tmp"

	policy := DefaultPolicy(projectRoot, runtimeDir, tempDir, nil)

	if policy.ProjectRoot != projectRoot {
		t.Errorf("expected ProjectRoot=%q, got %q", projectRoot, policy.ProjectRoot)
	}
	if policy.RuntimeDir != runtimeDir {
		t.Errorf("expected RuntimeDir=%q, got %q", runtimeDir, policy.RuntimeDir)
	}
	if policy.TempDir != tempDir {
		t.Errorf("expected TempDir=%q, got %q", tempDir, policy.TempDir)
	}
}

func TestDefaultPolicy_NetworkIsOutbound(t *testing.T) {
	policy := DefaultPolicy("/tmp/proj", "/tmp/rt", "/tmp", nil)

	if policy.Network != NetworkOutbound {
		t.Errorf("expected Network=%q, got %q", NetworkOutbound, policy.Network)
	}
}

func TestDefaultPolicy_AllowSubprocess(t *testing.T) {
	policy := DefaultPolicy("/tmp/proj", "/tmp/rt", "/tmp", nil)

	if !policy.AllowSubprocess {
		t.Error("expected AllowSubprocess=true, got false")
	}
}

func TestDefaultPolicy_CleanEnv(t *testing.T) {
	policy := DefaultPolicy("/tmp/proj", "/tmp/rt", "/tmp", nil)

	if policy.CleanEnv {
		t.Error("expected CleanEnv=false, got true")
	}
}

func TestDefaultPolicy_Env(t *testing.T) {
	env := []string{"PATH=/usr/bin", "HOME=/home/user"}
	policy := DefaultPolicy("/tmp/proj", "/tmp/rt", "/tmp", env)

	if len(policy.Env) != 2 {
		t.Fatalf("expected 2 env vars, got %d", len(policy.Env))
	}
	if policy.Env[0] != "PATH=/usr/bin" {
		t.Errorf("expected Env[0]=%q, got %q", "PATH=/usr/bin", policy.Env[0])
	}
}

func TestNoopSandbox_Apply_ReturnsNil(t *testing.T) {
	s := &noopSandbox{}
	cmd := exec.Command("echo", "hello")
	policy := DefaultPolicy("/tmp/proj", "/tmp/rt", "/tmp", nil)

	err := s.Apply(cmd, policy, "/tmp/rt")
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
}

func TestNewSandbox_ReturnsNonNil(t *testing.T) {
	s := NewSandbox()
	if s == nil {
		t.Error("expected NewSandbox() to return non-nil Sandbox")
	}
}

func TestNetworkModeConstants(t *testing.T) {
	if NetworkOutbound != "outbound" {
		t.Errorf("expected NetworkOutbound=%q, got %q", "outbound", NetworkOutbound)
	}
	if NetworkNone != "none" {
		t.Errorf("expected NetworkNone=%q, got %q", "none", NetworkNone)
	}
	if NetworkUnrestricted != "unrestricted" {
		t.Errorf("expected NetworkUnrestricted=%q, got %q", "unrestricted", NetworkUnrestricted)
	}
}

func TestDefaultPolicy_NilAgentModule(t *testing.T) {
	policy := DefaultPolicy("/tmp/proj", "/tmp/rt", "/tmp", nil)

	if policy.AgentModule != nil {
		t.Error("expected AgentModule=nil for default policy")
	}
}

func TestDefaultPolicy_EmptyExtraDenied(t *testing.T) {
	policy := DefaultPolicy("/tmp/proj", "/tmp/rt", "/tmp", nil)

	if len(policy.ExtraDenied) != 0 {
		t.Errorf("expected empty ExtraDenied, got %v", policy.ExtraDenied)
	}
}

func TestDetectGuardConflicts(t *testing.T) {
	results := []seatbelt.GuardResult{
		{
			Name: "node-toolchain",
			Rules: []seatbelt.Rule{
				seatbelt.AllowRule(`(allow file-read* (literal "/Users/test/.npmrc"))`),
			},
		},
		{
			Name: "npm",
			Rules: []seatbelt.Rule{
				seatbelt.DenyRule(`(deny file-read-data (literal "/Users/test/.npmrc"))`),
			},
		},
	}

	warnings := DetectGuardConflicts(results)
	if len(warnings) == 0 {
		t.Error("expected conflict warning for .npmrc")
	}

	found := false
	for _, w := range warnings {
		if strings.Contains(w, ".npmrc") && strings.Contains(w, "npm") && strings.Contains(w, "node-toolchain") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected warning mentioning .npmrc conflict between npm and node-toolchain, got: %v", warnings)
	}
}

func TestDetectGuardConflicts_NoConflict(t *testing.T) {
	results := []seatbelt.GuardResult{
		{
			Name: "filesystem",
			Rules: []seatbelt.Rule{
				seatbelt.AllowRule(`(allow file-read* (subpath "/project"))`),
			},
		},
	}

	warnings := DetectGuardConflicts(results)
	if len(warnings) != 0 {
		t.Errorf("expected no warnings, got: %v", warnings)
	}
}

// helper
func assertContains(t *testing.T, slice []string, item string, msg string) {
	t.Helper()
	for _, s := range slice {
		if s == item {
			return
		}
	}
	t.Errorf("%s: %q not found in %v", msg, item, slice)
}
