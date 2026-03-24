// Cloud provider credential guards for macOS Seatbelt profiles.
//
// Protects credentials for AWS, GCP, Azure, DigitalOcean, and OCI.

package guards

import (
	"fmt"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

// --- cloud-aws ---

type cloudAWSGuard struct{}

// CloudAWSGuard returns a Guard that denies access to AWS credentials.
func CloudAWSGuard() seatbelt.Guard { return &cloudAWSGuard{} }

func (g *cloudAWSGuard) Name() string        { return "cloud-aws" }
func (g *cloudAWSGuard) Type() string        { return "default" }
func (g *cloudAWSGuard) Description() string { return "Blocks access to AWS credentials and config" }

func (g *cloudAWSGuard) Rules(ctx *seatbelt.Context) seatbelt.GuardResult {
	result := seatbelt.GuardResult{}

	credsPath := EnvOverridePath(ctx, "AWS_SHARED_CREDENTIALS_FILE", ".aws/credentials")
	configPath := EnvOverridePath(ctx, "AWS_CONFIG_FILE", ".aws/config")

	// Check for env overrides
	if val, ok := ctx.EnvLookup("AWS_SHARED_CREDENTIALS_FILE"); ok && val != "" {
		result.Overrides = append(result.Overrides, seatbelt.Override{
			EnvVar:      "AWS_SHARED_CREDENTIALS_FILE",
			Value:       val,
			DefaultPath: ctx.HomePath(".aws/credentials"),
		})
	}
	if val, ok := ctx.EnvLookup("AWS_CONFIG_FILE"); ok && val != "" {
		result.Overrides = append(result.Overrides, seatbelt.Override{
			EnvVar:      "AWS_CONFIG_FILE",
			Value:       val,
			DefaultPath: ctx.HomePath(".aws/config"),
		})
	}

	if pathExists(credsPath) {
		result.Rules = append(result.Rules, DenyFile(credsPath)...)
		result.Protected = append(result.Protected, credsPath)
	} else {
		result.Skipped = append(result.Skipped, fmt.Sprintf("%s not found", credsPath))
	}

	if pathExists(configPath) {
		result.Rules = append(result.Rules, DenyFile(configPath)...)
		result.Protected = append(result.Protected, configPath)
	} else {
		result.Skipped = append(result.Skipped, fmt.Sprintf("%s not found", configPath))
	}

	// SSO cache and CLI cache are always denied by subpath
	ssoCache := ctx.HomePath(".aws/sso/cache")
	cliCache := ctx.HomePath(".aws/cli/cache")

	if dirExists(ssoCache) {
		result.Rules = append(result.Rules, DenyDir(ssoCache)...)
		result.Protected = append(result.Protected, ssoCache)
	} else {
		result.Skipped = append(result.Skipped, fmt.Sprintf("%s not found", ssoCache))
	}

	if dirExists(cliCache) {
		result.Rules = append(result.Rules, DenyDir(cliCache)...)
		result.Protected = append(result.Protected, cliCache)
	} else {
		result.Skipped = append(result.Skipped, fmt.Sprintf("%s not found", cliCache))
	}

	if len(result.Rules) > 0 {
		result.Rules = append([]seatbelt.Rule{seatbelt.SectionDeny("AWS credentials")}, result.Rules...)
	}

	return result
}

// --- cloud-gcp ---

type cloudGCPGuard struct{}

// CloudGCPGuard returns a Guard that denies access to GCP credentials.
func CloudGCPGuard() seatbelt.Guard { return &cloudGCPGuard{} }

func (g *cloudGCPGuard) Name() string        { return "cloud-gcp" }
func (g *cloudGCPGuard) Type() string        { return "default" }
func (g *cloudGCPGuard) Description() string { return "Blocks access to GCP credentials and config" }

func (g *cloudGCPGuard) Rules(ctx *seatbelt.Context) seatbelt.GuardResult {
	result := seatbelt.GuardResult{}
	gcloudPath := EnvOverridePath(ctx, "CLOUDSDK_CONFIG", ".config/gcloud")

	// Check for env override
	if val, ok := ctx.EnvLookup("CLOUDSDK_CONFIG"); ok && val != "" {
		result.Overrides = append(result.Overrides, seatbelt.Override{
			EnvVar:      "CLOUDSDK_CONFIG",
			Value:       val,
			DefaultPath: ctx.HomePath(".config/gcloud"),
		})
	}

	if dirExists(gcloudPath) {
		result.Rules = append(result.Rules, seatbelt.SectionDeny("GCP credentials"))
		result.Rules = append(result.Rules, DenyDir(gcloudPath)...)
		result.Protected = append(result.Protected, gcloudPath)
	} else {
		result.Skipped = append(result.Skipped, fmt.Sprintf("%s not found", gcloudPath))
	}

	// GOOGLE_APPLICATION_CREDENTIALS points to a single file
	if saPath, ok := ctx.EnvLookup("GOOGLE_APPLICATION_CREDENTIALS"); ok && saPath != "" {
		if pathExists(saPath) {
			result.Rules = append(result.Rules, DenyFile(saPath)...)
			result.Protected = append(result.Protected, saPath)
		} else {
			result.Skipped = append(result.Skipped, fmt.Sprintf("%s not found", saPath))
		}
		result.Overrides = append(result.Overrides, seatbelt.Override{
			EnvVar: "GOOGLE_APPLICATION_CREDENTIALS",
			Value:  saPath,
		})
	}

	return result
}

// --- cloud-azure ---

type cloudAzureGuard struct{}

// CloudAzureGuard returns a Guard that denies access to Azure credentials.
func CloudAzureGuard() seatbelt.Guard { return &cloudAzureGuard{} }

func (g *cloudAzureGuard) Name() string        { return "cloud-azure" }
func (g *cloudAzureGuard) Type() string        { return "default" }
func (g *cloudAzureGuard) Description() string { return "Blocks access to Azure CLI credentials" }

func (g *cloudAzureGuard) Rules(ctx *seatbelt.Context) seatbelt.GuardResult {
	result := seatbelt.GuardResult{}
	azurePath := EnvOverridePath(ctx, "AZURE_CONFIG_DIR", ".azure")

	// Check for env override
	if val, ok := ctx.EnvLookup("AZURE_CONFIG_DIR"); ok && val != "" {
		result.Overrides = append(result.Overrides, seatbelt.Override{
			EnvVar:      "AZURE_CONFIG_DIR",
			Value:       val,
			DefaultPath: ctx.HomePath(".azure"),
		})
	}

	if dirExists(azurePath) {
		result.Rules = append(result.Rules, seatbelt.SectionDeny("Azure credentials"))
		result.Rules = append(result.Rules, DenyDir(azurePath)...)
		result.Protected = append(result.Protected, azurePath)
	} else {
		result.Skipped = append(result.Skipped, fmt.Sprintf("%s not found", azurePath))
	}

	return result
}

// --- cloud-digitalocean ---

type cloudDigitalOceanGuard struct{}

// CloudDigitalOceanGuard returns a Guard that denies access to DigitalOcean credentials.
func CloudDigitalOceanGuard() seatbelt.Guard { return &cloudDigitalOceanGuard{} }

func (g *cloudDigitalOceanGuard) Name() string        { return "cloud-digitalocean" }
func (g *cloudDigitalOceanGuard) Type() string        { return "default" }
func (g *cloudDigitalOceanGuard) Description() string { return "Blocks access to DigitalOcean CLI credentials" }

func (g *cloudDigitalOceanGuard) Rules(ctx *seatbelt.Context) seatbelt.GuardResult {
	result := seatbelt.GuardResult{}
	doctlDir := ctx.HomePath(".config/doctl")

	if dirExists(doctlDir) {
		result.Rules = append(result.Rules, seatbelt.SectionDeny("DigitalOcean credentials"))
		result.Rules = append(result.Rules, DenyDir(doctlDir)...)
		result.Protected = append(result.Protected, doctlDir)
	} else {
		result.Skipped = append(result.Skipped, fmt.Sprintf("%s not found", doctlDir))
	}

	return result
}

// --- cloud-oci ---

type cloudOCIGuard struct{}

// CloudOCIGuard returns a Guard that denies access to Oracle Cloud credentials.
func CloudOCIGuard() seatbelt.Guard { return &cloudOCIGuard{} }

func (g *cloudOCIGuard) Name() string        { return "cloud-oci" }
func (g *cloudOCIGuard) Type() string        { return "default" }
func (g *cloudOCIGuard) Description() string { return "Blocks access to Oracle Cloud CLI credentials" }

func (g *cloudOCIGuard) Rules(ctx *seatbelt.Context) seatbelt.GuardResult {
	result := seatbelt.GuardResult{}

	// OCI_CLI_CONFIG_FILE points to a single file; the default ~/.oci is a directory.
	if ociFile, ok := ctx.EnvLookup("OCI_CLI_CONFIG_FILE"); ok && ociFile != "" {
		if pathExists(ociFile) {
			result.Rules = append(result.Rules, seatbelt.SectionDeny("OCI credentials"))
			result.Rules = append(result.Rules, DenyFile(ociFile)...)
			result.Protected = append(result.Protected, ociFile)
		} else {
			result.Skipped = append(result.Skipped, fmt.Sprintf("%s not found", ociFile))
		}
		result.Overrides = append(result.Overrides, seatbelt.Override{
			EnvVar:      "OCI_CLI_CONFIG_FILE",
			Value:       ociFile,
			DefaultPath: ctx.HomePath(".oci"),
		})
	} else {
		ociDir := ctx.HomePath(".oci")
		if dirExists(ociDir) {
			result.Rules = append(result.Rules, seatbelt.SectionDeny("OCI credentials"))
			result.Rules = append(result.Rules, DenyDir(ociDir)...)
			result.Protected = append(result.Protected, ociDir)
		} else {
			result.Skipped = append(result.Skipped, fmt.Sprintf("%s not found", ociDir))
		}
	}

	return result
}
