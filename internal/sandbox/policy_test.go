package sandbox

import (
	"os"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/pkg/seatbelt/modules"
)

func boolPtr(b bool) *bool { return &b }

func TestPolicyFromConfig_Nil_ReturnsDefaults(t *testing.T) {
	projectRoot := "/tmp/myproject"
	runtimeDir := "/tmp/aide-12345"
	homeDir := "/home/testuser"
	tempDir := "/tmp"

	policy, _, err := PolicyFromConfig(nil, projectRoot, runtimeDir, homeDir, tempDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if policy == nil {
		t.Fatal("expected non-nil policy for nil config")
	}

	defaults := DefaultPolicy(projectRoot, runtimeDir, tempDir, nil)

	assertSliceEqual(t, policy.Guards, defaults.Guards, "Guards")

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

func TestPolicyFromConfig_NetworkOverride(t *testing.T) {
	cfg := &config.SandboxPolicy{
		Network: &config.NetworkPolicy{Mode: "none"},
	}

	policy, _, err := PolicyFromConfig(cfg, "/tmp/proj", "/tmp/rt", "/home/user", "/tmp")
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

	policy, _, err := PolicyFromConfig(cfg, "/tmp/proj", "/tmp/rt", "/home/user", "/tmp")
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

	policy, _, err := PolicyFromConfig(cfg, "/tmp/proj", "/tmp/rt", "/home/user", "/tmp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !policy.CleanEnv {
		t.Error("expected CleanEnv=true, got false")
	}
}

func TestPolicyFromConfig_PartialOverride(t *testing.T) {
	cfg := &config.SandboxPolicy{
		Network: &config.NetworkPolicy{Mode: "none"},
	}

	projectRoot := "/tmp/proj"
	runtimeDir := "/tmp/rt"
	homeDir := "/home/user"
	tempDir := "/tmp"

	policy, _, err := PolicyFromConfig(cfg, projectRoot, runtimeDir, homeDir, tempDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	defaults := DefaultPolicy(projectRoot, runtimeDir, tempDir, nil)

	// Network should be overridden
	if policy.Network != NetworkNone {
		t.Errorf("expected Network=%q, got %q", NetworkNone, policy.Network)
	}

	// Guards should be defaults
	assertSliceEqual(t, policy.Guards, defaults.Guards, "Guards")
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

func TestPolicyFromConfig_DeniedAndDeniedExtra(t *testing.T) {
	homeDir := t.TempDir()
	kubeDir := homeDir + "/.kube"
	if err := os.MkdirAll(kubeDir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// DeniedExtra should append to defaults
	cfg := &config.SandboxPolicy{
		DeniedExtra: []string{"~/.kube"},
	}

	policy, _, err := PolicyFromConfig(cfg, "/tmp/proj", "/tmp/rt", homeDir, "/tmp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertContains(t, policy.ExtraDenied, kubeDir, "ExtraDenied should contain extra entry ~/.kube")

	// Denied override should replace
	cfg2 := &config.SandboxPolicy{
		Denied: []string{"~/.custom_denied_*"},
	}

	policy2, _, err := PolicyFromConfig(cfg2, "/tmp/proj", "/tmp/rt", "/home/user", "/tmp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(policy2.ExtraDenied) != 1 || policy2.ExtraDenied[0] != "/home/user/.custom_denied_*" {
		t.Errorf("expected ExtraDenied=[/home/user/.custom_denied_*], got %v", policy2.ExtraDenied)
	}

	// Both denied and denied_extra: denied wins
	cfg3 := &config.SandboxPolicy{
		Denied:      []string{"~/.custom_denied_*"},
		DeniedExtra: []string{"~/.extra_denied_*"},
	}

	policy3, _, err := PolicyFromConfig(cfg3, "/tmp/proj", "/tmp/rt", "/home/user", "/tmp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(policy3.ExtraDenied) != 1 || policy3.ExtraDenied[0] != "/home/user/.custom_denied_*" {
		t.Errorf("expected ExtraDenied=[/home/user/.custom_denied_*] (override wins), got %v", policy3.ExtraDenied)
	}
}

func TestPolicyFromConfig_NetworkPorts_Extracted(t *testing.T) {
	cfg := &config.SandboxPolicy{
		Network: &config.NetworkPolicy{
			Mode:       "outbound",
			AllowPorts: []int{443, 53},
		},
	}

	policy, _, err := PolicyFromConfig(cfg, "/tmp/proj", "/tmp/rt", "/home/user", "/tmp")
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

func TestPolicyFromConfig_GlobsNotValidated(t *testing.T) {
	cfg := &config.SandboxPolicy{
		Denied: []string{"~/.ssh/id_*", "~/.config/{foo}"},
	}
	policy, warnings, err := PolicyFromConfig(cfg, "/tmp/proj", "/tmp/rt", "/home/user", "/tmp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(policy.ExtraDenied) != 2 {
		t.Errorf("expected 2 denied paths (globs pass through), got %d", len(policy.ExtraDenied))
	}
	if len(warnings) != 0 {
		t.Errorf("expected no warnings for glob paths, got %v", warnings)
	}
}

// --- Guard resolution tests ---

func TestPolicyFromConfig_GuardsOverridesDefault(t *testing.T) {
	cfg := &config.SandboxPolicy{
		Guards: []string{"ssh-keys", "browsers"},
	}

	policy, _, err := PolicyFromConfig(cfg, "/tmp/proj", "/tmp/rt", "/home/user", "/tmp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should contain always guards + listed guards
	for _, g := range modules.AllGuards() {
		if g.Type() == "always" {
			assertContains(t, policy.Guards, g.Name(), "Guards should contain always guard "+g.Name())
		}
	}
	assertContains(t, policy.Guards, "ssh-keys", "Guards should contain ssh-keys")
	assertContains(t, policy.Guards, "browsers", "Guards should contain browsers")

	// Should NOT contain default guards that weren't listed
	defaults := modules.DefaultGuardNames()
	for _, name := range defaults {
		g, _ := modules.GuardByName(name)
		if g.Type() == "default" && name != "ssh-keys" && name != "browsers" {
			for _, gn := range policy.Guards {
				if gn == name {
					t.Errorf("Guards should not contain default guard %q when guards is set", name)
				}
			}
		}
	}
}

func TestPolicyFromConfig_GuardsExtraAdds(t *testing.T) {
	cfg := &config.SandboxPolicy{
		GuardsExtra: []string{"docker", "npm"},
	}

	policy, _, err := PolicyFromConfig(cfg, "/tmp/proj", "/tmp/rt", "/home/user", "/tmp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should contain all defaults + docker + npm
	defaults := modules.DefaultGuardNames()
	for _, name := range defaults {
		assertContains(t, policy.Guards, name, "Guards should contain default guard "+name)
	}
	assertContains(t, policy.Guards, "docker", "Guards should contain docker")
	assertContains(t, policy.Guards, "npm", "Guards should contain npm")
}

func TestPolicyFromConfig_UnguardRemoves(t *testing.T) {
	cfg := &config.SandboxPolicy{
		Unguard: []string{"browsers"},
	}

	policy, _, err := PolicyFromConfig(cfg, "/tmp/proj", "/tmp/rt", "/home/user", "/tmp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, g := range policy.Guards {
		if g == "browsers" {
			t.Error("Guards should not contain browsers after unguard")
		}
	}
}

func TestPolicyFromConfig_UnguardAlways_Error(t *testing.T) {
	cfg := &config.SandboxPolicy{
		Unguard: []string{"base"},
	}

	_, _, err := PolicyFromConfig(cfg, "/tmp/proj", "/tmp/rt", "/home/user", "/tmp")
	if err == nil {
		t.Error("expected error when unguarding always guard, got nil")
	}
	if !strings.Contains(err.Error(), "cannot unguard") {
		t.Errorf("expected 'cannot unguard' error, got: %v", err)
	}
}

func TestPolicyFromConfig_GuardsAndGuardsExtraWarns(t *testing.T) {
	cfg := &config.SandboxPolicy{
		Guards:      []string{"ssh-keys"},
		GuardsExtra: []string{"docker"},
	}

	_, warnings, err := PolicyFromConfig(cfg, "/tmp/proj", "/tmp/rt", "/home/user", "/tmp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found := false
	for _, w := range warnings {
		if strings.Contains(w, "guards") && strings.Contains(w, "guards_extra") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected warning about guards + guards_extra, got: %v", warnings)
	}
}

func TestPolicyFromConfig_UnknownGuardName_Error(t *testing.T) {
	cfg := &config.SandboxPolicy{
		Guards: []string{"nonexistent-guard"},
	}

	_, _, err := PolicyFromConfig(cfg, "/tmp/proj", "/tmp/rt", "/home/user", "/tmp")
	if err == nil {
		t.Error("expected error for unknown guard name, got nil")
	}
	if !strings.Contains(err.Error(), "unknown guard name") {
		t.Errorf("expected 'unknown guard name' error, got: %v", err)
	}
}

func TestPolicyFromConfig_CloudMetaGuard(t *testing.T) {
	cfg := &config.SandboxPolicy{
		Guards: []string{"cloud"},
	}

	policy, _, err := PolicyFromConfig(cfg, "/tmp/proj", "/tmp/rt", "/home/user", "/tmp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertContains(t, policy.Guards, "cloud-aws", "cloud meta-guard should expand to cloud-aws")
	assertContains(t, policy.Guards, "cloud-gcp", "cloud meta-guard should expand to cloud-gcp")
	assertContains(t, policy.Guards, "cloud-azure", "cloud meta-guard should expand to cloud-azure")
	assertContains(t, policy.Guards, "cloud-digitalocean", "cloud meta-guard should expand to cloud-digitalocean")
	assertContains(t, policy.Guards, "cloud-oci", "cloud meta-guard should expand to cloud-oci")
}

func TestValidateSandboxConfig_UnknownGuardInValidation(t *testing.T) {
	cfg := &config.SandboxPolicy{
		Guards: []string{"nonexistent"},
	}
	result := ValidateSandboxConfigDetailed(cfg)
	if len(result.Errors) == 0 {
		t.Error("expected validation error for unknown guard name")
	}
}

func TestValidateSandboxConfig_UnguardAlwaysInValidation(t *testing.T) {
	cfg := &config.SandboxPolicy{
		Unguard: []string{"base"},
	}
	result := ValidateSandboxConfigDetailed(cfg)
	if len(result.Errors) == 0 {
		t.Error("expected validation error for unguarding always guard")
	}
}

func TestValidateSandboxConfig_BothGuardsAndGuardsExtra_Warning(t *testing.T) {
	cfg := &config.SandboxPolicy{
		Guards:      []string{"ssh-keys"},
		GuardsExtra: []string{"docker"},
	}
	result := ValidateSandboxConfigDetailed(cfg)
	found := false
	for _, w := range result.Warnings {
		if strings.Contains(w, "guards") && strings.Contains(w, "guards_extra") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected warning about guards + guards_extra, got: %v", result.Warnings)
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
