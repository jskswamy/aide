// Cloud provider credential guards for macOS Seatbelt profiles.
//
// Protects credentials for AWS, GCP, Azure, DigitalOcean, and OCI.

package guards

import (
	"github.com/jskswamy/aide/pkg/seatbelt"
)

// --- cloud-aws ---

type cloudAWSGuard struct{}

// CloudAWSGuard returns a Guard that denies access to AWS credentials.
func CloudAWSGuard() seatbelt.Guard { return &cloudAWSGuard{} }

func (g *cloudAWSGuard) Name() string        { return "cloud-aws" }
func (g *cloudAWSGuard) Type() string        { return "default" }
func (g *cloudAWSGuard) Description() string { return "Blocks access to AWS credentials and config" }

func (g *cloudAWSGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	credsPath := EnvOverridePath(ctx, "AWS_SHARED_CREDENTIALS_FILE", ".aws/credentials")
	configPath := EnvOverridePath(ctx, "AWS_CONFIG_FILE", ".aws/config")

	var rules []seatbelt.Rule
	rules = append(rules, seatbelt.Section("AWS credentials"))
	rules = append(rules, DenyFile(credsPath)...)
	rules = append(rules, DenyFile(configPath)...)
	// SSO cache and CLI cache are always denied by subpath
	rules = append(rules, DenyDir(ctx.HomePath(".aws/sso/cache"))...)
	rules = append(rules, DenyDir(ctx.HomePath(".aws/cli/cache"))...)
	return rules
}

// --- cloud-gcp ---

type cloudGCPGuard struct{}

// CloudGCPGuard returns a Guard that denies access to GCP credentials.
func CloudGCPGuard() seatbelt.Guard { return &cloudGCPGuard{} }

func (g *cloudGCPGuard) Name() string        { return "cloud-gcp" }
func (g *cloudGCPGuard) Type() string        { return "default" }
func (g *cloudGCPGuard) Description() string { return "Blocks access to GCP credentials and config" }

func (g *cloudGCPGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	gcloudPath := EnvOverridePath(ctx, "CLOUDSDK_CONFIG", ".config/gcloud")

	var rules []seatbelt.Rule
	rules = append(rules, seatbelt.Section("GCP credentials"))
	rules = append(rules, DenyDir(gcloudPath)...)

	// GOOGLE_APPLICATION_CREDENTIALS points to a single file
	if saPath, ok := ctx.EnvLookup("GOOGLE_APPLICATION_CREDENTIALS"); ok && saPath != "" {
		rules = append(rules, DenyFile(saPath)...)
	}
	return rules
}

// --- cloud-azure ---

type cloudAzureGuard struct{}

// CloudAzureGuard returns a Guard that denies access to Azure credentials.
func CloudAzureGuard() seatbelt.Guard { return &cloudAzureGuard{} }

func (g *cloudAzureGuard) Name() string        { return "cloud-azure" }
func (g *cloudAzureGuard) Type() string        { return "default" }
func (g *cloudAzureGuard) Description() string { return "Blocks access to Azure CLI credentials" }

func (g *cloudAzureGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	azurePath := EnvOverridePath(ctx, "AZURE_CONFIG_DIR", ".azure")

	var rules []seatbelt.Rule
	rules = append(rules, seatbelt.Section("Azure credentials"))
	rules = append(rules, DenyDir(azurePath)...)
	return rules
}

// --- cloud-digitalocean ---

type cloudDigitalOceanGuard struct{}

// CloudDigitalOceanGuard returns a Guard that denies access to DigitalOcean credentials.
func CloudDigitalOceanGuard() seatbelt.Guard { return &cloudDigitalOceanGuard{} }

func (g *cloudDigitalOceanGuard) Name() string        { return "cloud-digitalocean" }
func (g *cloudDigitalOceanGuard) Type() string        { return "default" }
func (g *cloudDigitalOceanGuard) Description() string { return "Blocks access to DigitalOcean CLI credentials" }

func (g *cloudDigitalOceanGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	var rules []seatbelt.Rule
	rules = append(rules, seatbelt.Section("DigitalOcean credentials"))
	rules = append(rules, DenyDir(ctx.HomePath(".config/doctl"))...)
	return rules
}

// --- cloud-oci ---

type cloudOCIGuard struct{}

// CloudOCIGuard returns a Guard that denies access to Oracle Cloud credentials.
func CloudOCIGuard() seatbelt.Guard { return &cloudOCIGuard{} }

func (g *cloudOCIGuard) Name() string        { return "cloud-oci" }
func (g *cloudOCIGuard) Type() string        { return "default" }
func (g *cloudOCIGuard) Description() string { return "Blocks access to Oracle Cloud CLI credentials" }

func (g *cloudOCIGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	var rules []seatbelt.Rule
	rules = append(rules, seatbelt.Section("OCI credentials"))

	// OCI_CLI_CONFIG_FILE points to a single file; the default ~/.oci is a directory.
	if ociFile, ok := ctx.EnvLookup("OCI_CLI_CONFIG_FILE"); ok && ociFile != "" {
		rules = append(rules, DenyFile(ociFile)...)
	} else {
		rules = append(rules, DenyDir(ctx.HomePath(".oci"))...)
	}
	return rules
}
