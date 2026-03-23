// Cloud provider credential guards for macOS Seatbelt profiles.
//
// Protects credentials for AWS, GCP, Azure, DigitalOcean, and OCI.

package guards

import (
	"path/filepath"
	"strings"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

// denyPathRules returns deny rules for each path using subpath for directories
// and literal for files. Each path gets both file-read-data and file-write* denies.
func denyPathRules(paths []string) []seatbelt.Rule {
	var rules []seatbelt.Rule
	for _, p := range paths {
		rules = append(rules,
			seatbelt.Raw(`(deny file-read-data
    `+seatbelt.HomeSubpath("", p)+`
)`),
			seatbelt.Raw(`(deny file-write*
    `+seatbelt.HomeSubpath("", p)+`
)`),
		)
	}
	return rules
}

// denyLiteralRules returns deny rules for each path using literal match.
// Each path gets both file-read-data and file-write* denies.
func denyLiteralRules(paths []string) []seatbelt.Rule {
	var rules []seatbelt.Rule
	for _, p := range paths {
		rules = append(rules,
			seatbelt.Raw(`(deny file-read-data
    `+seatbelt.HomeLiteral("", p)+`
)`),
			seatbelt.Raw(`(deny file-write*
    `+seatbelt.HomeLiteral("", p)+`
)`),
		)
	}
	return rules
}

// envOverridePath returns the value of envKey from ctx, falling back to
// filepath.Join(ctx.HomeDir, defaultRel) if not set.
func envOverridePath(ctx *seatbelt.Context, envKey, defaultRel string) string {
	if v, ok := ctx.EnvLookup(envKey); ok && v != "" {
		return v
	}
	return ctx.HomePath(defaultRel)
}

// splitColonPaths splits a colon-separated list of paths (e.g. KUBECONFIG).
func splitColonPaths(s string) []string {
	return strings.Split(s, ":")
}

// denySubpathRuleForPath returns deny rules using subpath for a single absolute path.
func denySubpathRuleForPath(p string) []seatbelt.Rule {
	return []seatbelt.Rule{
		seatbelt.Raw(`(deny file-read-data
    ` + `(subpath "` + p + `")` + `
)`),
		seatbelt.Raw(`(deny file-write*
    ` + `(subpath "` + p + `")` + `
)`),
	}
}

// denyLiteralRuleForPath returns deny rules using literal for a single absolute path.
func denyLiteralRuleForPath(p string) []seatbelt.Rule {
	return []seatbelt.Rule{
		seatbelt.Raw(`(deny file-read-data
    ` + `(literal "` + filepath.Clean(p) + `")` + `
)`),
		seatbelt.Raw(`(deny file-write*
    ` + `(literal "` + filepath.Clean(p) + `")` + `
)`),
	}
}

// CloudGuardNames returns all guard names provided by this file and related guards.
func CloudGuardNames() []string {
	return []string{
		"cloud-aws",
		"cloud-gcp",
		"cloud-azure",
		"cloud-digitalocean",
		"cloud-oci",
		"kubernetes",
		"terraform",
		"vault",
	}
}

// --- cloud-aws ---

type cloudAWSGuard struct{}

// CloudAWSGuard returns a Guard that denies access to AWS credentials.
func CloudAWSGuard() seatbelt.Guard { return &cloudAWSGuard{} }

func (g *cloudAWSGuard) Name() string        { return "cloud-aws" }
func (g *cloudAWSGuard) Type() string        { return "default" }
func (g *cloudAWSGuard) Description() string { return "AWS credentials and config files" }

func (g *cloudAWSGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	credsPath := envOverridePath(ctx, "AWS_SHARED_CREDENTIALS_FILE", ".aws/credentials")
	configPath := envOverridePath(ctx, "AWS_CONFIG_FILE", ".aws/config")

	var rules []seatbelt.Rule
	rules = append(rules, seatbelt.Section("AWS credentials"))
	rules = append(rules, denyLiteralRuleForPath(credsPath)...)
	rules = append(rules, denyLiteralRuleForPath(configPath)...)
	// SSO cache and CLI cache are always denied by subpath
	rules = append(rules, denySubpathRuleForPath(ctx.HomePath(".aws/sso/cache"))...)
	rules = append(rules, denySubpathRuleForPath(ctx.HomePath(".aws/cli/cache"))...)
	return rules
}

// --- cloud-gcp ---

type cloudGCPGuard struct{}

// CloudGCPGuard returns a Guard that denies access to GCP credentials.
func CloudGCPGuard() seatbelt.Guard { return &cloudGCPGuard{} }

func (g *cloudGCPGuard) Name() string        { return "cloud-gcp" }
func (g *cloudGCPGuard) Type() string        { return "default" }
func (g *cloudGCPGuard) Description() string { return "GCP gcloud config directory and application credentials" }

func (g *cloudGCPGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	gcloudPath := envOverridePath(ctx, "CLOUDSDK_CONFIG", ".config/gcloud")

	var rules []seatbelt.Rule
	rules = append(rules, seatbelt.Section("GCP credentials"))
	rules = append(rules, denySubpathRuleForPath(gcloudPath)...)

	// GOOGLE_APPLICATION_CREDENTIALS points to a single file
	if saPath, ok := ctx.EnvLookup("GOOGLE_APPLICATION_CREDENTIALS"); ok && saPath != "" {
		rules = append(rules, denyLiteralRuleForPath(saPath)...)
	}
	return rules
}

// --- cloud-azure ---

type cloudAzureGuard struct{}

// CloudAzureGuard returns a Guard that denies access to Azure credentials.
func CloudAzureGuard() seatbelt.Guard { return &cloudAzureGuard{} }

func (g *cloudAzureGuard) Name() string        { return "cloud-azure" }
func (g *cloudAzureGuard) Type() string        { return "default" }
func (g *cloudAzureGuard) Description() string { return "Azure CLI config directory" }

func (g *cloudAzureGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	azurePath := envOverridePath(ctx, "AZURE_CONFIG_DIR", ".azure")

	var rules []seatbelt.Rule
	rules = append(rules, seatbelt.Section("Azure credentials"))
	rules = append(rules, denySubpathRuleForPath(azurePath)...)
	return rules
}

// --- cloud-digitalocean ---

type cloudDigitalOceanGuard struct{}

// CloudDigitalOceanGuard returns a Guard that denies access to DigitalOcean credentials.
func CloudDigitalOceanGuard() seatbelt.Guard { return &cloudDigitalOceanGuard{} }

func (g *cloudDigitalOceanGuard) Name() string        { return "cloud-digitalocean" }
func (g *cloudDigitalOceanGuard) Type() string        { return "default" }
func (g *cloudDigitalOceanGuard) Description() string { return "DigitalOcean doctl config directory" }

func (g *cloudDigitalOceanGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	var rules []seatbelt.Rule
	rules = append(rules, seatbelt.Section("DigitalOcean credentials"))
	rules = append(rules, denySubpathRuleForPath(ctx.HomePath(".config/doctl"))...)
	return rules
}

// --- cloud-oci ---

type cloudOCIGuard struct{}

// CloudOCIGuard returns a Guard that denies access to Oracle Cloud credentials.
func CloudOCIGuard() seatbelt.Guard { return &cloudOCIGuard{} }

func (g *cloudOCIGuard) Name() string        { return "cloud-oci" }
func (g *cloudOCIGuard) Type() string        { return "default" }
func (g *cloudOCIGuard) Description() string { return "Oracle Cloud CLI config directory" }

func (g *cloudOCIGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	ociPath := envOverridePath(ctx, "OCI_CLI_CONFIG_FILE", ".oci")

	var rules []seatbelt.Rule
	rules = append(rules, seatbelt.Section("OCI credentials"))
	rules = append(rules, denySubpathRuleForPath(ociPath)...)
	return rules
}
