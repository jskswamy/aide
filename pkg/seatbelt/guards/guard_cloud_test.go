package guards_test

import (
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
	g := guards.CloudAWSGuard()
	ctx := &seatbelt.Context{HomeDir: "/home/testuser"}
	output := renderTestRules(g.Rules(ctx).Rules)

	for _, want := range []string{
		"/home/testuser/.aws/credentials",
		"/home/testuser/.aws/config",
		"/home/testuser/.aws/sso/cache",
		"/home/testuser/.aws/cli/cache",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("expected output to contain %q", want)
		}
	}
}

func TestGuard_CloudAWS_EnvOverrideCredentials(t *testing.T) {
	g := guards.CloudAWSGuard()
	ctx := &seatbelt.Context{
		HomeDir: "/home/testuser",
		Env:     []string{"AWS_SHARED_CREDENTIALS_FILE=/custom/creds"},
	}
	output := renderTestRules(g.Rules(ctx).Rules)

	if !strings.Contains(output, "/custom/creds") {
		t.Error("expected env override path /custom/creds in output")
	}
}

func TestGuard_CloudAWS_EnvOverrideConfig(t *testing.T) {
	g := guards.CloudAWSGuard()
	ctx := &seatbelt.Context{
		HomeDir: "/home/testuser",
		Env:     []string{"AWS_CONFIG_FILE=/custom/config"},
	}
	output := renderTestRules(g.Rules(ctx).Rules)

	if !strings.Contains(output, "/custom/config") {
		t.Error("expected env override path /custom/config in output")
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
	g := guards.CloudGCPGuard()
	ctx := &seatbelt.Context{HomeDir: "/home/testuser"}
	output := renderTestRules(g.Rules(ctx).Rules)

	if !strings.Contains(output, "/home/testuser/.config/gcloud") {
		t.Error("expected ~/.config/gcloud in output")
	}
}

func TestGuard_CloudGCP_EnvOverride(t *testing.T) {
	g := guards.CloudGCPGuard()
	ctx := &seatbelt.Context{
		HomeDir: "/home/testuser",
		Env:     []string{"CLOUDSDK_CONFIG=/custom/gcloud"},
	}
	output := renderTestRules(g.Rules(ctx).Rules)

	if !strings.Contains(output, "/custom/gcloud") {
		t.Error("expected env override /custom/gcloud in output")
	}
}

func TestGuard_CloudGCP_ApplicationCredentials(t *testing.T) {
	g := guards.CloudGCPGuard()
	ctx := &seatbelt.Context{
		HomeDir: "/home/testuser",
		Env:     []string{"GOOGLE_APPLICATION_CREDENTIALS=/tmp/sa.json"},
	}
	output := renderTestRules(g.Rules(ctx).Rules)

	if !strings.Contains(output, "/tmp/sa.json") {
		t.Error("expected GOOGLE_APPLICATION_CREDENTIALS path in output")
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
	g := guards.CloudAzureGuard()
	ctx := &seatbelt.Context{HomeDir: "/home/testuser"}
	output := renderTestRules(g.Rules(ctx).Rules)

	if !strings.Contains(output, "/home/testuser/.azure") {
		t.Error("expected ~/.azure in output")
	}
}

func TestGuard_CloudAzure_EnvOverride(t *testing.T) {
	g := guards.CloudAzureGuard()
	ctx := &seatbelt.Context{
		HomeDir: "/home/testuser",
		Env:     []string{"AZURE_CONFIG_DIR=/custom/azure"},
	}
	output := renderTestRules(g.Rules(ctx).Rules)

	if !strings.Contains(output, "/custom/azure") {
		t.Error("expected env override /custom/azure in output")
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
	g := guards.CloudDigitalOceanGuard()
	ctx := &seatbelt.Context{HomeDir: "/home/testuser"}
	output := renderTestRules(g.Rules(ctx).Rules)

	if !strings.Contains(output, "/home/testuser/.config/doctl") {
		t.Error("expected ~/.config/doctl in output")
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
	g := guards.CloudOCIGuard()
	ctx := &seatbelt.Context{HomeDir: "/home/testuser"}
	output := renderTestRules(g.Rules(ctx).Rules)

	if !strings.Contains(output, "/home/testuser/.oci") {
		t.Error("expected ~/.oci in output")
	}
}

func TestGuard_CloudOCI_EnvOverride(t *testing.T) {
	g := guards.CloudOCIGuard()
	ctx := &seatbelt.Context{
		HomeDir: "/home/testuser",
		Env:     []string{"OCI_CLI_CONFIG_FILE=/custom/oci/config"},
	}
	output := renderTestRules(g.Rules(ctx).Rules)

	if !strings.Contains(output, "/custom/oci/config") {
		t.Error("expected env override /custom/oci/config in output")
	}
}

// --- kubernetes ---

func TestGuard_Kubernetes_Metadata(t *testing.T) {
	g := guards.KubernetesGuard()
	if g.Name() != "kubernetes" {
		t.Errorf("expected Name() = %q, got %q", "kubernetes", g.Name())
	}
	if g.Type() != "default" {
		t.Errorf("expected Type() = %q, got %q", "default", g.Type())
	}
}

func TestGuard_Kubernetes_DefaultPaths(t *testing.T) {
	g := guards.KubernetesGuard()
	ctx := &seatbelt.Context{HomeDir: "/home/testuser"}
	output := renderTestRules(g.Rules(ctx).Rules)

	if !strings.Contains(output, "/home/testuser/.kube/config") {
		t.Error("expected ~/.kube/config in output")
	}
}

func TestGuard_Kubernetes_KubeconfigColonSplit(t *testing.T) {
	g := guards.KubernetesGuard()
	ctx := &seatbelt.Context{
		HomeDir: "/home/testuser",
		Env:     []string{"KUBECONFIG=/path/a:/path/b:/path/c"},
	}
	output := renderTestRules(g.Rules(ctx).Rules)

	for _, want := range []string{"/path/a", "/path/b", "/path/c"} {
		if !strings.Contains(output, want) {
			t.Errorf("expected KUBECONFIG path %q in output", want)
		}
	}
	// Default path should not appear when env overrides
	if strings.Contains(output, "/home/testuser/.kube/config") {
		t.Error("default kube config path should not appear when KUBECONFIG env is set")
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
	g := guards.TerraformGuard()
	ctx := &seatbelt.Context{HomeDir: "/home/testuser"}
	output := renderTestRules(g.Rules(ctx).Rules)

	for _, want := range []string{
		"/home/testuser/.terraform.d/credentials.tfrc.json",
		"/home/testuser/.terraformrc",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("expected %q in output", want)
		}
	}
}

func TestGuard_Terraform_EnvOverride(t *testing.T) {
	g := guards.TerraformGuard()
	ctx := &seatbelt.Context{
		HomeDir: "/home/testuser",
		Env:     []string{"TF_CLI_CONFIG_FILE=/custom/terraform.rc"},
	}
	output := renderTestRules(g.Rules(ctx).Rules)

	if !strings.Contains(output, "/custom/terraform.rc") {
		t.Error("expected env override /custom/terraform.rc in output")
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
	g := guards.VaultGuard()
	ctx := &seatbelt.Context{HomeDir: "/home/testuser"}
	output := renderTestRules(g.Rules(ctx).Rules)

	if !strings.Contains(output, "/home/testuser/.vault-token") {
		t.Error("expected ~/.vault-token in output")
	}
}

func TestGuard_Vault_EnvOverride(t *testing.T) {
	g := guards.VaultGuard()
	ctx := &seatbelt.Context{
		HomeDir: "/home/testuser",
		Env:     []string{"VAULT_TOKEN_FILE=/custom/vault-token"},
	}
	output := renderTestRules(g.Rules(ctx).Rules)

	if !strings.Contains(output, "/custom/vault-token") {
		t.Error("expected env override /custom/vault-token in output")
	}
}

func TestGuard_Kubernetes_EmptySegments(t *testing.T) {
	g := guards.KubernetesGuard()
	ctx := &seatbelt.Context{
		HomeDir: "/Users/testuser",
		Env:     []string{"KUBECONFIG=/path/a::/path/b:"},
	}
	output := renderTestRules(g.Rules(ctx).Rules)
	if strings.Contains(output, `""`) {
		t.Error("empty KUBECONFIG segment should not produce empty path rule")
	}
	for _, want := range []string{"/path/a", "/path/b"} {
		if !strings.Contains(output, want) {
			t.Errorf("expected %q in output", want)
		}
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
