package sandbox

import (
	"testing"

	"github.com/jskswamy/aide/internal/config"
)

func boolPtr(b bool) *bool { return &b }

func TestPolicyFromConfig_Nil_ReturnsDefaults(t *testing.T) {
	projectRoot := "/tmp/myproject"
	runtimeDir := "/tmp/aide-12345"
	homeDir := "/home/testuser"
	tempDir := "/tmp"

	policy, err := PolicyFromConfig(nil, projectRoot, runtimeDir, homeDir, tempDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if policy == nil {
		t.Fatal("expected non-nil policy for nil config")
	}

	defaults := DefaultPolicy(projectRoot, runtimeDir, homeDir, tempDir)

	// Verify writable matches defaults
	assertSliceEqual(t, policy.Writable, defaults.Writable, "Writable")
	assertSliceEqual(t, policy.Readable, defaults.Readable, "Readable")
	assertSliceEqual(t, policy.Denied, defaults.Denied, "Denied")

	if policy.Network != defaults.Network {
		t.Errorf("expected Network=%q, got %q", defaults.Network, policy.Network)
	}
	if policy.AllowSubprocess != defaults.AllowSubprocess {
		t.Errorf("expected AllowSubprocess=%v, got %v", defaults.AllowSubprocess, policy.AllowSubprocess)
	}
	if policy.CleanEnv != defaults.CleanEnv {
		t.Errorf("expected CleanEnv=%v, got %v", defaults.CleanEnv, policy.CleanEnv)
	}
}

func TestPolicyFromConfig_Disabled_ReturnsNil(t *testing.T) {
	cfg := &config.SandboxPolicy{Disabled: true}

	policy, err := PolicyFromConfig(cfg, "/tmp/proj", "/tmp/rt", "/home/user", "/tmp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if policy != nil {
		t.Error("expected nil policy for disabled sandbox")
	}
}

func TestPolicyFromConfig_WritableOverride(t *testing.T) {
	cfg := &config.SandboxPolicy{
		Writable: []string{"/custom/writable"},
	}

	policy, err := PolicyFromConfig(cfg, "/tmp/proj", "/tmp/rt", "/home/user", "/tmp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(policy.Writable) != 1 || policy.Writable[0] != "/custom/writable" {
		t.Errorf("expected Writable=[/custom/writable], got %v", policy.Writable)
	}

	// Other fields should keep defaults
	defaults := DefaultPolicy("/tmp/proj", "/tmp/rt", "/home/user", "/tmp")
	assertSliceEqual(t, policy.Readable, defaults.Readable, "Readable")
	assertSliceEqual(t, policy.Denied, defaults.Denied, "Denied")
}

func TestPolicyFromConfig_ReadableOverride(t *testing.T) {
	cfg := &config.SandboxPolicy{
		Readable: []string{"/custom/readable"},
	}

	policy, err := PolicyFromConfig(cfg, "/tmp/proj", "/tmp/rt", "/home/user", "/tmp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(policy.Readable) != 1 || policy.Readable[0] != "/custom/readable" {
		t.Errorf("expected Readable=[/custom/readable], got %v", policy.Readable)
	}
}

func TestPolicyFromConfig_DeniedOverride(t *testing.T) {
	cfg := &config.SandboxPolicy{
		Denied: []string{"/custom/denied"},
	}

	policy, err := PolicyFromConfig(cfg, "/tmp/proj", "/tmp/rt", "/home/user", "/tmp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(policy.Denied) != 1 || policy.Denied[0] != "/custom/denied" {
		t.Errorf("expected Denied=[/custom/denied], got %v", policy.Denied)
	}
}

func TestPolicyFromConfig_NetworkOverride(t *testing.T) {
	cfg := &config.SandboxPolicy{
		Network: "none",
	}

	policy, err := PolicyFromConfig(cfg, "/tmp/proj", "/tmp/rt", "/home/user", "/tmp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if policy.Network != NetworkNone {
		t.Errorf("expected Network=%q, got %q", NetworkNone, policy.Network)
	}
}

func TestPolicyFromConfig_AllowSubprocessOverride(t *testing.T) {
	cfg := &config.SandboxPolicy{
		AllowSubprocess: boolPtr(false),
	}

	policy, err := PolicyFromConfig(cfg, "/tmp/proj", "/tmp/rt", "/home/user", "/tmp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if policy.AllowSubprocess {
		t.Error("expected AllowSubprocess=false, got true")
	}
}

func TestPolicyFromConfig_CleanEnvOverride(t *testing.T) {
	cfg := &config.SandboxPolicy{
		CleanEnv: boolPtr(true),
	}

	policy, err := PolicyFromConfig(cfg, "/tmp/proj", "/tmp/rt", "/home/user", "/tmp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !policy.CleanEnv {
		t.Error("expected CleanEnv=true, got false")
	}
}

func TestPolicyFromConfig_TemplateResolution(t *testing.T) {
	cfg := &config.SandboxPolicy{
		Writable: []string{"{{ .project_root }}", "{{ .runtime_dir }}"},
	}

	policy, err := PolicyFromConfig(cfg, "/my/project", "/run/aide-99", "/home/user", "/tmp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertContains(t, policy.Writable, "/my/project", "Writable should contain resolved project_root")
	assertContains(t, policy.Writable, "/run/aide-99", "Writable should contain resolved runtime_dir")
}

func TestPolicyFromConfig_TildeExpansion(t *testing.T) {
	cfg := &config.SandboxPolicy{
		Readable: []string{"~/.gitconfig", "~/foo"},
	}

	policy, err := PolicyFromConfig(cfg, "/tmp/proj", "/tmp/rt", "/home/testuser", "/tmp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertContains(t, policy.Readable, "/home/testuser/.gitconfig", "Readable should contain expanded ~/.gitconfig")
	assertContains(t, policy.Readable, "/home/testuser/foo", "Readable should contain expanded ~/foo")
}

func TestPolicyFromConfig_PartialOverride(t *testing.T) {
	cfg := &config.SandboxPolicy{
		Network: "none",
	}

	projectRoot := "/tmp/proj"
	runtimeDir := "/tmp/rt"
	homeDir := "/home/user"
	tempDir := "/tmp"

	policy, err := PolicyFromConfig(cfg, projectRoot, runtimeDir, homeDir, tempDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	defaults := DefaultPolicy(projectRoot, runtimeDir, homeDir, tempDir)

	// Network should be overridden
	if policy.Network != NetworkNone {
		t.Errorf("expected Network=%q, got %q", NetworkNone, policy.Network)
	}

	// Everything else should be defaults
	assertSliceEqual(t, policy.Writable, defaults.Writable, "Writable")
	assertSliceEqual(t, policy.Readable, defaults.Readable, "Readable")
	assertSliceEqual(t, policy.Denied, defaults.Denied, "Denied")
	if policy.AllowSubprocess != defaults.AllowSubprocess {
		t.Errorf("expected AllowSubprocess=%v, got %v", defaults.AllowSubprocess, policy.AllowSubprocess)
	}
	if policy.CleanEnv != defaults.CleanEnv {
		t.Errorf("expected CleanEnv=%v, got %v", defaults.CleanEnv, policy.CleanEnv)
	}
}

func TestResolvePaths_InvalidTemplate(t *testing.T) {
	vars := map[string]string{
		"project_root": "/proj",
		"runtime_dir":  "/rt",
		"home":         "/home/user",
		"config_dir":   "/home/user/.config/aide",
	}

	_, err := ResolvePaths([]string{"{{ .nonexistent }}"}, vars)
	if err == nil {
		t.Error("expected error for invalid template variable, got nil")
	}
}

func TestResolvePaths_HomeTemplate(t *testing.T) {
	vars := map[string]string{
		"project_root": "/proj",
		"runtime_dir":  "/rt",
		"home":         "/home/user",
		"config_dir":   "/home/user/.config/aide",
	}

	result, err := ResolvePaths([]string{"{{ .home }}/.local"}, vars)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 1 || result[0] != "/home/user/.local" {
		t.Errorf("expected [/home/user/.local], got %v", result)
	}
}

func TestResolvePaths_ConfigDir(t *testing.T) {
	vars := map[string]string{
		"project_root": "/proj",
		"runtime_dir":  "/rt",
		"home":         "/home/user",
		"config_dir":   "/home/user/.config/aide",
	}

	result, err := ResolvePaths([]string{"{{ .config_dir }}/plugins"}, vars)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 1 || result[0] != "/home/user/.config/aide/plugins" {
		t.Errorf("expected [/home/user/.config/aide/plugins], got %v", result)
	}
}

func TestValidateSandboxConfig_InvalidNetwork(t *testing.T) {
	cfg := &config.SandboxPolicy{
		Network: "foobar",
	}

	err := ValidateSandboxConfig(cfg)
	if err == nil {
		t.Error("expected validation error for invalid network mode, got nil")
	}
}

func TestValidateSandboxConfig_ValidModes(t *testing.T) {
	validModes := []string{"outbound", "none", "unrestricted", ""}
	for _, mode := range validModes {
		cfg := &config.SandboxPolicy{Network: mode}
		if err := ValidateSandboxConfig(cfg); err != nil {
			t.Errorf("unexpected error for network mode %q: %v", mode, err)
		}
	}
}

func TestValidateSandboxConfig_Nil(t *testing.T) {
	if err := ValidateSandboxConfig(nil); err != nil {
		t.Errorf("unexpected error for nil config: %v", err)
	}
}

func TestValidateSandboxConfig_Disabled(t *testing.T) {
	cfg := &config.SandboxPolicy{Disabled: true}
	if err := ValidateSandboxConfig(cfg); err != nil {
		t.Errorf("unexpected error for disabled config: %v", err)
	}
}

// helper
func assertSliceEqual(t *testing.T, got, want []string, name string) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("%s: expected %d items, got %d\n  want: %v\n  got:  %v", name, len(want), len(got), want, got)
		return
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("%s[%d]: expected %q, got %q", name, i, want[i], got[i])
		}
	}
}
