//go:build darwin

package sandbox

import (
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/config"
)

// Contract tests verify that config fields actually produce rules in the
// rendered seatbelt profile. Catches "parsed but dropped" bugs.

func renderProfileFromConfig(t *testing.T, cfg *config.SandboxPolicy) string {
	t.Helper()
	policy, _, err := PolicyFromConfig(cfg, "/project", "/runtime", "/Users/testuser", "/tmp")
	if err != nil {
		t.Fatalf("PolicyFromConfig failed: %v", err)
	}
	sb := &darwinSandbox{}
	profile, err := sb.GenerateProfile(*policy)
	if err != nil {
		t.Fatalf("GenerateProfile failed: %v", err)
	}
	return profile
}

func TestContract_WritableExtraProducesRule(t *testing.T) {
	dir := t.TempDir()
	profile := renderProfileFromConfig(t, &config.SandboxPolicy{
		WritableExtra: []string{dir},
	})
	if !strings.Contains(profile, dir) {
		t.Error("writable_extra path not found in rendered profile")
	}
	if !strings.Contains(profile, "file-write*") {
		t.Error("expected file-write* rule for writable_extra")
	}
}

func TestContract_ReadableExtraProducesRule(t *testing.T) {
	dir := t.TempDir()
	profile := renderProfileFromConfig(t, &config.SandboxPolicy{
		ReadableExtra: []string{dir},
	})
	if !strings.Contains(profile, dir) {
		t.Error("readable_extra path not found in rendered profile")
	}
}

func TestContract_DeniedExtraProducesRule(t *testing.T) {
	dir := t.TempDir()
	profile := renderProfileFromConfig(t, &config.SandboxPolicy{
		DeniedExtra: []string{dir},
	})
	if !strings.Contains(profile, dir) {
		t.Error("denied_extra path not found in rendered profile")
	}
	if !strings.Contains(profile, "deny file-read-data") {
		t.Error("expected deny file-read-data for denied path")
	}
	if !strings.Contains(profile, "deny file-write*") {
		t.Error("expected deny file-write* for denied path")
	}
}

func TestContract_AllowSubprocessFalseProducesDenyFork(t *testing.T) {
	f := false
	profile := renderProfileFromConfig(t, &config.SandboxPolicy{
		AllowSubprocess: &f,
	})
	if !strings.Contains(profile, "(deny process-fork)") {
		t.Error("allow_subprocess: false should produce deny process-fork")
	}
	if strings.Contains(profile, "(allow process-fork)") {
		t.Error("allow_subprocess: false should NOT produce allow process-fork")
	}
}

func TestContract_AllowSubprocessTrueDefault(t *testing.T) {
	profile := renderProfileFromConfig(t, &config.SandboxPolicy{})
	if !strings.Contains(profile, "(allow process-fork)") {
		t.Error("default policy should have allow process-fork")
	}
}

func TestContract_NetworkModeApplied(t *testing.T) {
	profile := renderProfileFromConfig(t, &config.SandboxPolicy{
		Network: &config.NetworkPolicy{Mode: "none"},
	})
	if strings.Contains(profile, "(allow network-outbound)") {
		t.Error("network: none should NOT have allow network-outbound")
	}
}
