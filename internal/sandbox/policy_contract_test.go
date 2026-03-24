//go:build darwin

package sandbox

import (
	"os"
	"path/filepath"
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

func TestContract_ReadableExtraOptOutsDeny(t *testing.T) {
	// Create a temp project dir with a .env file
	projectDir := t.TempDir()
	envFile := filepath.Join(projectDir, ".env")
	if err := os.WriteFile(envFile, []byte("SECRET=val"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Render with the .env file in readable_extra — the project-secrets guard
	// should skip it instead of denying it.
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	policy, _, err := PolicyFromConfig(&config.SandboxPolicy{
		ReadableExtra: []string{envFile},
	}, projectDir, "/runtime", home, "/tmp")
	if err != nil {
		t.Fatalf("PolicyFromConfig failed: %v", err)
	}
	sb := &darwinSandbox{}
	profile, err := sb.GenerateProfile(*policy)
	if err != nil {
		t.Fatalf("GenerateProfile failed: %v", err)
	}

	// The .env file should appear as an allow (readable_extra), not a deny.
	// Check each line: no deny line should reference the env file.
	for _, line := range strings.Split(profile, "\n") {
		if strings.Contains(line, "deny") && strings.Contains(line, envFile) {
			t.Errorf("readable_extra path %q should opt out of deny rules, but found: %s", envFile, strings.TrimSpace(line))
		}
	}
	if !strings.Contains(profile, envFile) {
		t.Errorf("readable_extra path %q should appear in profile as an allow rule", envFile)
	}
}

func TestContract_ScopedHomeReads(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	profile := renderProfileFromConfig(t, &config.SandboxPolicy{})

	// Scoped home reads should include these development directories
	for _, dir := range []string{".config", ".cache", ".ssh"} {
		expected := filepath.Join(home, dir)
		if !strings.Contains(profile, expected) {
			t.Errorf("default profile should contain scoped home read for %q", expected)
		}
	}

	// Should NOT contain ~/Documents as a subpath
	docsPath := filepath.Join(home, "Documents")
	if strings.Contains(profile, `(subpath "`+docsPath+`")`) {
		t.Error("default profile should NOT allow reads to ~/Documents")
	}
}

func TestContract_MountedVolumesDenied(t *testing.T) {
	if _, err := os.Stat("/Volumes"); err != nil {
		t.Skip("/Volumes not found on this machine")
	}

	profile := renderProfileFromConfig(t, &config.SandboxPolicy{})

	if !strings.Contains(profile, "/Volumes") {
		t.Error("default profile should contain deny rules for /Volumes")
	}
	if !strings.Contains(profile, `deny file-read-data (subpath "/Volumes")`) {
		t.Error("default profile should deny file-read-data for /Volumes")
	}
}

func TestContract_CrossGuardSafety_NodeToolchain(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	profile := renderProfileFromConfig(t, &config.SandboxPolicy{})

	// Node-toolchain write paths (.npm, .yarn, .nvm) should appear as
	// allow rules and must NOT be denied by any deny guard.
	for _, dir := range []string{".npm", ".yarn", ".nvm"} {
		full := filepath.Join(home, dir)
		if !strings.Contains(profile, full) {
			t.Errorf("default profile should contain node-toolchain path %q", full)
		}
		// Check that no deny rule targets this specific path
		denyPattern := `deny file-write* (subpath "` + full + `")`
		if strings.Contains(profile, denyPattern) {
			t.Errorf("node-toolchain path %q should NOT be denied by any guard", full)
		}
	}
}

func TestContract_CrossGuardSafety_NixToolchain(t *testing.T) {
	if _, err := os.Stat("/nix/store"); err != nil {
		t.Skip("/nix/store not found — nix not installed")
	}

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	profile := renderProfileFromConfig(t, &config.SandboxPolicy{})

	// Nix-toolchain write paths should appear as allow rules and must NOT
	// be denied by any deny guard.
	for _, dir := range []string{".nix-profile", ".cache/nix"} {
		full := filepath.Join(home, dir)
		if !strings.Contains(profile, full) {
			t.Errorf("default profile should contain nix-toolchain path %q", full)
		}
		denyPattern := `deny file-write* (subpath "` + full + `")`
		if strings.Contains(profile, denyPattern) {
			t.Errorf("nix-toolchain path %q should NOT be denied by any guard", full)
		}
	}
}
