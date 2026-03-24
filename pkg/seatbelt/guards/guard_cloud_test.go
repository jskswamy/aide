package guards_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt"
	"github.com/jskswamy/aide/pkg/seatbelt/guards"
)

// --- cloud-aws ---

func TestGuard_CloudAWS_Metadata(t *testing.T) {
	g := guards.CloudAWSGuard()
	if g.Name() != "cloud-aws" {
		t.Errorf("expected Name() = %q, got %q", "cloud-aws", g.Name())
	}
	if g.Type() != "default" {
		t.Errorf("expected Type() = %q, got %q", "default", g.Type())
	}
}

func TestGuard_CloudAWS_DefaultPaths(t *testing.T) {
	home := t.TempDir()
	// Create all AWS credential paths
	os.MkdirAll(filepath.Join(home, ".aws/sso/cache"), 0o755)
	os.MkdirAll(filepath.Join(home, ".aws/cli/cache"), 0o755)
	os.WriteFile(filepath.Join(home, ".aws/credentials"), []byte("fake"), 0o600)
	os.WriteFile(filepath.Join(home, ".aws/config"), []byte("fake"), 0o600)

	g := guards.CloudAWSGuard()
	ctx := &seatbelt.Context{HomeDir: home}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	for _, want := range []string{
		filepath.Join(home, ".aws/credentials"),
		filepath.Join(home, ".aws/config"),
		filepath.Join(home, ".aws/sso/cache"),
		filepath.Join(home, ".aws/cli/cache"),
	} {
		if !strings.Contains(output, want) {
			t.Errorf("expected output to contain %q", want)
		}
	}
	if len(result.Protected) == 0 {
		t.Error("expected Protected to be populated")
	}
}

func TestGuard_CloudAWS_AllSkipped(t *testing.T) {
	ctx := &seatbelt.Context{HomeDir: t.TempDir(), GOOS: "darwin"}
	result := guards.CloudAWSGuard().Rules(ctx)
	if len(result.Rules) != 0 {
		t.Errorf("expected 0 rules when paths missing, got %d", len(result.Rules))
	}
	if len(result.Skipped) == 0 {
		t.Error("expected skip messages")
	}
}

func TestGuard_CloudAWS_EnvOverrideCredentials(t *testing.T) {
	home := t.TempDir()
	customCreds := filepath.Join(home, "custom-creds")
	os.WriteFile(customCreds, []byte("fake"), 0o600)

	g := guards.CloudAWSGuard()
	ctx := &seatbelt.Context{
		HomeDir: home,
		Env:     []string{"AWS_SHARED_CREDENTIALS_FILE=" + customCreds},
	}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	if !strings.Contains(output, customCreds) {
		t.Error("expected env override path in output")
	}
	if len(result.Overrides) == 0 {
		t.Error("expected Overrides to be populated for AWS_SHARED_CREDENTIALS_FILE")
	}
}

func TestGuard_CloudAWS_EnvOverrideConfig(t *testing.T) {
	home := t.TempDir()
	customConfig := filepath.Join(home, "custom-config")
	os.WriteFile(customConfig, []byte("fake"), 0o600)

	g := guards.CloudAWSGuard()
	ctx := &seatbelt.Context{
		HomeDir: home,
		Env:     []string{"AWS_CONFIG_FILE=" + customConfig},
	}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	if !strings.Contains(output, customConfig) {
		t.Error("expected env override path in output")
	}
	if len(result.Overrides) == 0 {
		t.Error("expected Overrides to be populated for AWS_CONFIG_FILE")
	}
}

// --- cloud-gcp ---

func TestGuard_CloudGCP_Metadata(t *testing.T) {
	g := guards.CloudGCPGuard()
	if g.Name() != "cloud-gcp" {
		t.Errorf("expected Name() = %q, got %q", "cloud-gcp", g.Name())
	}
	if g.Type() != "default" {
		t.Errorf("expected Type() = %q, got %q", "default", g.Type())
	}
}

func TestGuard_CloudGCP_DefaultPaths(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, ".config/gcloud"), 0o755)

	g := guards.CloudGCPGuard()
	ctx := &seatbelt.Context{HomeDir: home}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	if !strings.Contains(output, filepath.Join(home, ".config/gcloud")) {
		t.Error("expected ~/.config/gcloud in output")
	}
	if len(result.Protected) == 0 {
		t.Error("expected Protected to be populated")
	}
}

func TestGuard_CloudGCP_AllSkipped(t *testing.T) {
	ctx := &seatbelt.Context{HomeDir: t.TempDir(), GOOS: "darwin"}
	result := guards.CloudGCPGuard().Rules(ctx)
	if len(result.Rules) != 0 {
		t.Errorf("expected 0 rules when paths missing, got %d", len(result.Rules))
	}
	if len(result.Skipped) == 0 {
		t.Error("expected skip messages")
	}
}

func TestGuard_CloudGCP_EnvOverride(t *testing.T) {
	home := t.TempDir()
	customGcloud := filepath.Join(home, "custom-gcloud")
	os.MkdirAll(customGcloud, 0o755)

	g := guards.CloudGCPGuard()
	ctx := &seatbelt.Context{
		HomeDir: home,
		Env:     []string{"CLOUDSDK_CONFIG=" + customGcloud},
	}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	if !strings.Contains(output, customGcloud) {
		t.Error("expected env override in output")
	}
	if len(result.Overrides) == 0 {
		t.Error("expected Overrides to be populated for CLOUDSDK_CONFIG")
	}
}

func TestGuard_CloudGCP_ApplicationCredentials(t *testing.T) {
	home := t.TempDir()
	saFile := filepath.Join(home, "sa.json")
	os.WriteFile(saFile, []byte("fake"), 0o600)

	g := guards.CloudGCPGuard()
	ctx := &seatbelt.Context{
		HomeDir: home,
		Env:     []string{"GOOGLE_APPLICATION_CREDENTIALS=" + saFile},
	}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	if !strings.Contains(output, saFile) {
		t.Error("expected GOOGLE_APPLICATION_CREDENTIALS path in output")
	}
	if len(result.Overrides) == 0 {
		t.Error("expected Overrides to be populated for GOOGLE_APPLICATION_CREDENTIALS")
	}
}

// --- cloud-azure ---

func TestGuard_CloudAzure_Metadata(t *testing.T) {
	g := guards.CloudAzureGuard()
	if g.Name() != "cloud-azure" {
		t.Errorf("expected Name() = %q, got %q", "cloud-azure", g.Name())
	}
	if g.Type() != "default" {
		t.Errorf("expected Type() = %q, got %q", "default", g.Type())
	}
}

func TestGuard_CloudAzure_DefaultPaths(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, ".azure"), 0o755)

	g := guards.CloudAzureGuard()
	ctx := &seatbelt.Context{HomeDir: home}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	if !strings.Contains(output, filepath.Join(home, ".azure")) {
		t.Error("expected ~/.azure in output")
	}
	if len(result.Protected) == 0 {
		t.Error("expected Protected to be populated")
	}
}

func TestGuard_CloudAzure_AllSkipped(t *testing.T) {
	ctx := &seatbelt.Context{HomeDir: t.TempDir(), GOOS: "darwin"}
	result := guards.CloudAzureGuard().Rules(ctx)
	if len(result.Rules) != 0 {
		t.Errorf("expected 0 rules when paths missing, got %d", len(result.Rules))
	}
	if len(result.Skipped) == 0 {
		t.Error("expected skip messages")
	}
}

func TestGuard_CloudAzure_EnvOverride(t *testing.T) {
	home := t.TempDir()
	customAzure := filepath.Join(home, "custom-azure")
	os.MkdirAll(customAzure, 0o755)

	g := guards.CloudAzureGuard()
	ctx := &seatbelt.Context{
		HomeDir: home,
		Env:     []string{"AZURE_CONFIG_DIR=" + customAzure},
	}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	if !strings.Contains(output, customAzure) {
		t.Error("expected env override in output")
	}
	if len(result.Overrides) == 0 {
		t.Error("expected Overrides to be populated for AZURE_CONFIG_DIR")
	}
}

// --- cloud-digitalocean ---

func TestGuard_CloudDigitalOcean_Metadata(t *testing.T) {
	g := guards.CloudDigitalOceanGuard()
	if g.Name() != "cloud-digitalocean" {
		t.Errorf("expected Name() = %q, got %q", "cloud-digitalocean", g.Name())
	}
	if g.Type() != "default" {
		t.Errorf("expected Type() = %q, got %q", "default", g.Type())
	}
}

func TestGuard_CloudDigitalOcean_DefaultPaths(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, ".config/doctl"), 0o755)

	g := guards.CloudDigitalOceanGuard()
	ctx := &seatbelt.Context{HomeDir: home}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	if !strings.Contains(output, filepath.Join(home, ".config/doctl")) {
		t.Error("expected ~/.config/doctl in output")
	}
	if len(result.Protected) == 0 {
		t.Error("expected Protected to be populated")
	}
}

func TestGuard_CloudDigitalOcean_AllSkipped(t *testing.T) {
	ctx := &seatbelt.Context{HomeDir: t.TempDir(), GOOS: "darwin"}
	result := guards.CloudDigitalOceanGuard().Rules(ctx)
	if len(result.Rules) != 0 {
		t.Errorf("expected 0 rules when paths missing, got %d", len(result.Rules))
	}
	if len(result.Skipped) == 0 {
		t.Error("expected skip messages")
	}
}

// --- cloud-oci ---

func TestGuard_CloudOCI_Metadata(t *testing.T) {
	g := guards.CloudOCIGuard()
	if g.Name() != "cloud-oci" {
		t.Errorf("expected Name() = %q, got %q", "cloud-oci", g.Name())
	}
	if g.Type() != "default" {
		t.Errorf("expected Type() = %q, got %q", "default", g.Type())
	}
}

func TestGuard_CloudOCI_DefaultPaths(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, ".oci"), 0o755)

	g := guards.CloudOCIGuard()
	ctx := &seatbelt.Context{HomeDir: home}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	if !strings.Contains(output, filepath.Join(home, ".oci")) {
		t.Error("expected ~/.oci in output")
	}
	if len(result.Protected) == 0 {
		t.Error("expected Protected to be populated")
	}
}

func TestGuard_CloudOCI_AllSkipped(t *testing.T) {
	ctx := &seatbelt.Context{HomeDir: t.TempDir(), GOOS: "darwin"}
	result := guards.CloudOCIGuard().Rules(ctx)
	if len(result.Rules) != 0 {
		t.Errorf("expected 0 rules when paths missing, got %d", len(result.Rules))
	}
	if len(result.Skipped) == 0 {
		t.Error("expected skip messages")
	}
}

func TestGuard_CloudOCI_EnvOverride(t *testing.T) {
	home := t.TempDir()
	customOCI := filepath.Join(home, "custom-oci-config")
	os.WriteFile(customOCI, []byte("fake"), 0o600)

	g := guards.CloudOCIGuard()
	ctx := &seatbelt.Context{
		HomeDir: home,
		Env:     []string{"OCI_CLI_CONFIG_FILE=" + customOCI},
	}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	if !strings.Contains(output, customOCI) {
		t.Error("expected env override in output")
	}
	if len(result.Overrides) == 0 {
		t.Error("expected Overrides to be populated for OCI_CLI_CONFIG_FILE")
	}
}

// --- kubernetes ---

func TestGuard_Kubernetes_Metadata(t *testing.T) {
	g := guards.KubernetesGuard()
	if g.Name() != "kubernetes" {
		t.Errorf("expected Name() = %q, got %q", "kubernetes", g.Name())
	}
	if g.Type() != "opt-in" {
		t.Errorf("expected Type() = %q, got %q", "opt-in", g.Type())
	}
}

func TestGuard_Kubernetes_DefaultPaths(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, ".kube"), 0o755)
	os.WriteFile(filepath.Join(home, ".kube/config"), []byte("fake"), 0o600)

	g := guards.KubernetesGuard()
	ctx := &seatbelt.Context{HomeDir: home}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	if !strings.Contains(output, filepath.Join(home, ".kube/config")) {
		t.Error("expected ~/.kube/config in output")
	}
	if len(result.Protected) == 0 {
		t.Error("expected Protected to be populated")
	}
}

func TestGuard_Kubernetes_AllSkipped(t *testing.T) {
	ctx := &seatbelt.Context{HomeDir: t.TempDir(), GOOS: "darwin"}
	result := guards.KubernetesGuard().Rules(ctx)
	if len(result.Rules) != 0 {
		t.Errorf("expected 0 rules when paths missing, got %d", len(result.Rules))
	}
	if len(result.Skipped) == 0 {
		t.Error("expected skip messages")
	}
}

func TestGuard_Kubernetes_KubeconfigColonSplit(t *testing.T) {
	home := t.TempDir()
	pathA := filepath.Join(home, "path-a")
	pathB := filepath.Join(home, "path-b")
	pathC := filepath.Join(home, "path-c")
	for _, p := range []string{pathA, pathB, pathC} {
		os.WriteFile(p, []byte("fake"), 0o600)
	}

	g := guards.KubernetesGuard()
	ctx := &seatbelt.Context{
		HomeDir: home,
		Env:     []string{"KUBECONFIG=" + pathA + ":" + pathB + ":" + pathC},
	}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	for _, want := range []string{pathA, pathB, pathC} {
		if !strings.Contains(output, want) {
			t.Errorf("expected KUBECONFIG path %q in output", want)
		}
	}
	// Default path should not appear when env overrides
	if strings.Contains(output, filepath.Join(home, ".kube/config")) {
		t.Error("default kube config path should not appear when KUBECONFIG env is set")
	}
	if len(result.Overrides) == 0 {
		t.Error("expected Overrides to be populated for KUBECONFIG")
	}
}

func TestGuard_Kubernetes_EmptySegments(t *testing.T) {
	home := t.TempDir()
	pathA := filepath.Join(home, "path-a")
	pathB := filepath.Join(home, "path-b")
	os.WriteFile(pathA, []byte("fake"), 0o600)
	os.WriteFile(pathB, []byte("fake"), 0o600)

	g := guards.KubernetesGuard()
	ctx := &seatbelt.Context{
		HomeDir: home,
		Env:     []string{"KUBECONFIG=" + pathA + "::" + pathB + ":"},
	}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)
	if strings.Contains(output, `""`) {
		t.Error("empty KUBECONFIG segment should not produce empty path rule")
	}
	for _, want := range []string{pathA, pathB} {
		if !strings.Contains(output, want) {
			t.Errorf("expected %q in output", want)
		}
	}
}

// --- terraform ---

func TestGuard_Terraform_Metadata(t *testing.T) {
	g := guards.TerraformGuard()
	if g.Name() != "terraform" {
		t.Errorf("expected Name() = %q, got %q", "terraform", g.Name())
	}
	if g.Type() != "default" {
		t.Errorf("expected Type() = %q, got %q", "default", g.Type())
	}
}

func TestGuard_Terraform_DefaultPaths(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, ".terraform.d"), 0o755)
	os.WriteFile(filepath.Join(home, ".terraform.d/credentials.tfrc.json"), []byte("fake"), 0o600)
	os.WriteFile(filepath.Join(home, ".terraformrc"), []byte("fake"), 0o600)

	g := guards.TerraformGuard()
	ctx := &seatbelt.Context{HomeDir: home}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	for _, want := range []string{
		filepath.Join(home, ".terraform.d/credentials.tfrc.json"),
		filepath.Join(home, ".terraformrc"),
	} {
		if !strings.Contains(output, want) {
			t.Errorf("expected %q in output", want)
		}
	}
	if len(result.Protected) == 0 {
		t.Error("expected Protected to be populated")
	}
}

func TestGuard_Terraform_AllSkipped(t *testing.T) {
	ctx := &seatbelt.Context{HomeDir: t.TempDir(), GOOS: "darwin"}
	result := guards.TerraformGuard().Rules(ctx)
	if len(result.Rules) != 0 {
		t.Errorf("expected 0 rules when paths missing, got %d", len(result.Rules))
	}
	if len(result.Skipped) == 0 {
		t.Error("expected skip messages")
	}
}

func TestGuard_Terraform_EnvOverride(t *testing.T) {
	home := t.TempDir()
	customTF := filepath.Join(home, "terraform.rc")
	os.WriteFile(customTF, []byte("fake"), 0o600)

	g := guards.TerraformGuard()
	ctx := &seatbelt.Context{
		HomeDir: home,
		Env:     []string{"TF_CLI_CONFIG_FILE=" + customTF},
	}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	if !strings.Contains(output, customTF) {
		t.Error("expected env override in output")
	}
	if len(result.Overrides) == 0 {
		t.Error("expected Overrides to be populated for TF_CLI_CONFIG_FILE")
	}
}

// --- vault ---

func TestGuard_Vault_Metadata(t *testing.T) {
	g := guards.VaultGuard()
	if g.Name() != "vault" {
		t.Errorf("expected Name() = %q, got %q", "vault", g.Name())
	}
	if g.Type() != "default" {
		t.Errorf("expected Type() = %q, got %q", "default", g.Type())
	}
}

func TestGuard_Vault_DefaultPaths(t *testing.T) {
	home := t.TempDir()
	os.WriteFile(filepath.Join(home, ".vault-token"), []byte("fake"), 0o600)

	g := guards.VaultGuard()
	ctx := &seatbelt.Context{HomeDir: home}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	if !strings.Contains(output, filepath.Join(home, ".vault-token")) {
		t.Error("expected ~/.vault-token in output")
	}
	if len(result.Protected) == 0 {
		t.Error("expected Protected to be populated")
	}
}

func TestGuard_Vault_AllSkipped(t *testing.T) {
	ctx := &seatbelt.Context{HomeDir: t.TempDir(), GOOS: "darwin"}
	result := guards.VaultGuard().Rules(ctx)
	if len(result.Rules) != 0 {
		t.Errorf("expected 0 rules when paths missing, got %d", len(result.Rules))
	}
	if len(result.Skipped) == 0 {
		t.Error("expected skip messages")
	}
}

func TestGuard_Vault_EnvOverride(t *testing.T) {
	home := t.TempDir()
	customVault := filepath.Join(home, "custom-vault-token")
	os.WriteFile(customVault, []byte("fake"), 0o600)

	g := guards.VaultGuard()
	ctx := &seatbelt.Context{
		HomeDir: home,
		Env:     []string{"VAULT_TOKEN_FILE=" + customVault},
	}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	if !strings.Contains(output, customVault) {
		t.Error("expected env override in output")
	}
	if len(result.Overrides) == 0 {
		t.Error("expected Overrides to be populated for VAULT_TOKEN_FILE")
	}
}

// --- CloudGuardNames ---

func TestCloudGuardNames(t *testing.T) {
	names := guards.CloudGuardNames()
	want := []string{"cloud-aws", "cloud-gcp", "cloud-azure", "cloud-digitalocean", "cloud-oci"}
	if len(names) != len(want) {
		t.Errorf("expected %d guard names, got %d: %v", len(want), len(names), names)
		return
	}
	nameSet := make(map[string]bool)
	for _, n := range names {
		nameSet[n] = true
	}
	for _, w := range want {
		if !nameSet[w] {
			t.Errorf("expected %q in CloudGuardNames()", w)
		}
	}
}
