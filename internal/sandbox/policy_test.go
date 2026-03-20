package sandbox

import (
	"strings"
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

func TestResolveSandboxRef_Disabled(t *testing.T) {
	ref := &config.SandboxRef{Disabled: true}

	_, disabled, err := ResolveSandboxRef(ref, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !disabled {
		t.Error("expected disabled=true for SandboxRef{Disabled: true}")
	}
}

func TestResolveSandboxRef_Nil_DefaultPolicy(t *testing.T) {
	cfg, disabled, err := ResolveSandboxRef(nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if disabled {
		t.Error("expected disabled=false for nil ref")
	}
	if cfg != nil {
		t.Error("expected nil config for nil ref (use defaults)")
	}
}

func TestResolveSandboxRef_ProfileNone_Disabled(t *testing.T) {
	ref := &config.SandboxRef{ProfileName: "none"}

	_, disabled, err := ResolveSandboxRef(ref, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !disabled {
		t.Error("expected disabled=true for profile 'none'")
	}
}

func TestResolveSandboxRef_ProfileDefault_DefaultPolicy(t *testing.T) {
	ref := &config.SandboxRef{ProfileName: "default"}

	cfg, disabled, err := ResolveSandboxRef(ref, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if disabled {
		t.Error("expected disabled=false for profile 'default'")
	}
	if cfg != nil {
		t.Error("expected nil config for profile 'default' (use defaults)")
	}
}

func TestResolveSandboxRef_NamedProfile(t *testing.T) {
	sandboxes := map[string]config.SandboxPolicy{
		"strict": {
			Network: &config.NetworkPolicy{Mode: "none"},
		},
	}
	ref := &config.SandboxRef{ProfileName: "strict"}

	cfg, disabled, err := ResolveSandboxRef(ref, sandboxes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if disabled {
		t.Error("expected disabled=false for named profile")
	}
	if cfg == nil {
		t.Fatal("expected non-nil config for named profile")
	}
	if cfg.Network == nil || cfg.Network.Mode != "none" {
		t.Errorf("expected network mode 'none', got %v", cfg.Network)
	}
}

func TestResolveSandboxRef_UnknownProfile_Error(t *testing.T) {
	ref := &config.SandboxRef{ProfileName: "nonexistent"}

	_, _, err := ResolveSandboxRef(ref, nil)
	if err == nil {
		t.Error("expected error for unknown profile, got nil")
	}
}

func TestResolveSandboxRef_Inline(t *testing.T) {
	ref := &config.SandboxRef{
		Inline: &config.SandboxPolicy{
			Writable: []string{"/custom"},
		},
	}

	cfg, disabled, err := ResolveSandboxRef(ref, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if disabled {
		t.Error("expected disabled=false for inline policy")
	}
	if cfg == nil || len(cfg.Writable) != 1 || cfg.Writable[0] != "/custom" {
		t.Errorf("expected Writable=[/custom], got %v", cfg)
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
		Network: &config.NetworkPolicy{Mode: "none"},
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
		Network: &config.NetworkPolicy{Mode: "none"},
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
		Network: &config.NetworkPolicy{Mode: "foobar"},
	}

	err := ValidateSandboxConfig(cfg)
	if err == nil {
		t.Error("expected validation error for invalid network mode, got nil")
	}
}

func TestValidateSandboxConfig_ValidModes(t *testing.T) {
	validModes := []string{"outbound", "none", "unrestricted", ""}
	for _, mode := range validModes {
		cfg := &config.SandboxPolicy{Network: &config.NetworkPolicy{Mode: mode}}
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

func TestValidateSandboxRef_Disabled(t *testing.T) {
	ref := &config.SandboxRef{Disabled: true}
	if err := ValidateSandboxRef(ref, nil); err != nil {
		t.Errorf("unexpected error for disabled sandbox ref: %v", err)
	}
}

func TestValidateSandboxRef_UnknownProfile(t *testing.T) {
	ref := &config.SandboxRef{ProfileName: "nonexistent"}
	if err := ValidateSandboxRef(ref, nil); err == nil {
		t.Error("expected error for unknown profile, got nil")
	}
}

func TestValidateSandboxRef_KnownProfile(t *testing.T) {
	sandboxes := map[string]config.SandboxPolicy{
		"strict": {Network: &config.NetworkPolicy{Mode: "none"}},
	}
	ref := &config.SandboxRef{ProfileName: "strict"}
	if err := ValidateSandboxRef(ref, sandboxes); err != nil {
		t.Errorf("unexpected error for known profile: %v", err)
	}
}

func TestPolicyFromConfig_DeniedExtra_AppendsToDefaults(t *testing.T) {
	projectRoot := "/tmp/proj"
	runtimeDir := "/tmp/rt"
	homeDir := "/home/user"
	tempDir := "/tmp"

	cfg := &config.SandboxPolicy{
		DeniedExtra: []string{"~/.kube"},
	}

	policy, err := PolicyFromConfig(cfg, projectRoot, runtimeDir, homeDir, tempDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	defaults := DefaultPolicy(projectRoot, runtimeDir, homeDir, tempDir)

	// Should contain all defaults
	for _, d := range defaults.Denied {
		assertContains(t, policy.Denied, d, "Denied should contain default entry")
	}
	// Should also contain the extra entry (tilde-expanded)
	assertContains(t, policy.Denied, "/home/user/.kube", "Denied should contain extra entry ~/.kube")

	// Total length should be defaults + 1
	if len(policy.Denied) != len(defaults.Denied)+1 {
		t.Errorf("expected %d denied entries, got %d: %v", len(defaults.Denied)+1, len(policy.Denied), policy.Denied)
	}
}

func TestPolicyFromConfig_DeniedOverride_ReplacesDefaults(t *testing.T) {
	cfg := &config.SandboxPolicy{
		Denied: []string{"/custom"},
	}

	policy, err := PolicyFromConfig(cfg, "/tmp/proj", "/tmp/rt", "/home/user", "/tmp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(policy.Denied) != 1 || policy.Denied[0] != "/custom" {
		t.Errorf("expected Denied=[/custom], got %v", policy.Denied)
	}
}

func TestPolicyFromConfig_BothDeniedAndExtra_OverrideWins(t *testing.T) {
	cfg := &config.SandboxPolicy{
		Denied:      []string{"/custom"},
		DeniedExtra: []string{"/extra"},
	}

	policy, err := PolicyFromConfig(cfg, "/tmp/proj", "/tmp/rt", "/home/user", "/tmp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(policy.Denied) != 1 || policy.Denied[0] != "/custom" {
		t.Errorf("expected Denied=[/custom] (override wins), got %v", policy.Denied)
	}
}

func TestPolicyFromConfig_ReadableExtra_AppendsToDefaults(t *testing.T) {
	projectRoot := "/tmp/proj"
	runtimeDir := "/tmp/rt"
	homeDir := "/home/user"
	tempDir := "/tmp"

	cfg := &config.SandboxPolicy{
		ReadableExtra: []string{"/opt/custom/lib"},
	}

	policy, err := PolicyFromConfig(cfg, projectRoot, runtimeDir, homeDir, tempDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	defaults := DefaultPolicy(projectRoot, runtimeDir, homeDir, tempDir)

	// Should contain all defaults
	for _, r := range defaults.Readable {
		assertContains(t, policy.Readable, r, "Readable should contain default entry")
	}
	// Should also contain the extra entry
	assertContains(t, policy.Readable, "/opt/custom/lib", "Readable should contain extra entry")

	if len(policy.Readable) != len(defaults.Readable)+1 {
		t.Errorf("expected %d readable entries, got %d: %v", len(defaults.Readable)+1, len(policy.Readable), policy.Readable)
	}
}

func TestPolicyFromConfig_WritableExtra_AppendsToDefaults(t *testing.T) {
	projectRoot := "/tmp/proj"
	runtimeDir := "/tmp/rt"
	homeDir := "/home/user"
	tempDir := "/tmp"

	cfg := &config.SandboxPolicy{
		WritableExtra: []string{"/var/log/myapp"},
	}

	policy, err := PolicyFromConfig(cfg, projectRoot, runtimeDir, homeDir, tempDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	defaults := DefaultPolicy(projectRoot, runtimeDir, homeDir, tempDir)

	// Should contain all defaults
	for _, w := range defaults.Writable {
		assertContains(t, policy.Writable, w, "Writable should contain default entry")
	}
	// Should also contain the extra entry
	assertContains(t, policy.Writable, "/var/log/myapp", "Writable should contain extra entry")

	if len(policy.Writable) != len(defaults.Writable)+1 {
		t.Errorf("expected %d writable entries, got %d: %v", len(defaults.Writable)+1, len(policy.Writable), policy.Writable)
	}
}

func TestPolicyFromConfig_NetworkPorts_Extracted(t *testing.T) {
	cfg := &config.SandboxPolicy{
		Network: &config.NetworkPolicy{
			Mode:       "outbound",
			AllowPorts: []int{443, 53},
		},
	}

	policy, err := PolicyFromConfig(cfg, "/tmp/proj", "/tmp/rt", "/home/user", "/tmp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(policy.AllowPorts) != 2 || policy.AllowPorts[0] != 443 || policy.AllowPorts[1] != 53 {
		t.Errorf("expected AllowPorts=[443, 53], got %v", policy.AllowPorts)
	}

	if policy.DenyPorts != nil {
		t.Errorf("expected DenyPorts=nil, got %v", policy.DenyPorts)
	}
}

func TestValidateSandboxConfig_InvalidPort(t *testing.T) {
	tests := []struct {
		name  string
		ports []int
		field string
	}{
		{"AllowPorts with port 0", []int{443, 0}, "allow_ports"},
		{"AllowPorts with port 70000", []int{70000}, "allow_ports"},
		{"DenyPorts with port 0", []int{0, 53}, "deny_ports"},
		{"DenyPorts with port 70000", []int{70000, 443}, "deny_ports"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cfg *config.SandboxPolicy
			if tt.field == "allow_ports" {
				cfg = &config.SandboxPolicy{
					Network: &config.NetworkPolicy{Mode: "outbound", AllowPorts: tt.ports},
				}
			} else {
				cfg = &config.SandboxPolicy{
					Network: &config.NetworkPolicy{Mode: "outbound", DenyPorts: tt.ports},
				}
			}
			result := ValidateSandboxConfigDetailed(cfg)
			if len(result.Errors) == 0 {
				t.Errorf("expected validation error for %s with ports %v, got none", tt.field, tt.ports)
			}
		})
	}
}

func TestValidateSandboxConfig_ValidPorts(t *testing.T) {
	cfg := &config.SandboxPolicy{
		Network: &config.NetworkPolicy{
			Mode:       "outbound",
			AllowPorts: []int{443, 53},
		},
	}
	result := ValidateSandboxConfigDetailed(cfg)
	if len(result.Errors) != 0 {
		t.Errorf("expected no errors for valid ports, got: %v", result.Errors)
	}
}

func TestValidateSandboxConfig_BothDeniedAndExtra_Warning(t *testing.T) {
	cfg := &config.SandboxPolicy{
		Denied:      []string{"/custom"},
		DeniedExtra: []string{"/extra"},
	}
	result := ValidateSandboxConfigDetailed(cfg)
	if len(result.Warnings) == 0 {
		t.Error("expected warning when both Denied and DeniedExtra are set, got none")
	}
	found := false
	for _, w := range result.Warnings {
		if strings.Contains(w, "denied") && strings.Contains(w, "denied_extra") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected warning about denied + denied_extra, got: %v", result.Warnings)
	}
}

func TestValidateSandboxConfig_BroadWritable_Warning(t *testing.T) {
	tests := []struct {
		name     string
		writable []string
	}{
		{"tilde home", []string{"~"}},
		{"home dir path", []string{"/home/user"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.SandboxPolicy{
				Writable: tt.writable,
			}
			result := ValidateSandboxConfigDetailed(cfg)
			if len(result.Warnings) == 0 {
				t.Errorf("expected warning for broad writable %v, got none", tt.writable)
			}
		})
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
