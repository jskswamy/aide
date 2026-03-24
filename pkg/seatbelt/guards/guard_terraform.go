// Terraform guard for macOS Seatbelt profiles.
//
// Protects Terraform credential files from leakage.

package guards

import (
	"fmt"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

type terraformGuard struct{}

// TerraformGuard returns a Guard that denies access to Terraform credential files.
func TerraformGuard() seatbelt.Guard { return &terraformGuard{} }

func (g *terraformGuard) Name() string        { return "terraform" }
func (g *terraformGuard) Type() string        { return "default" }
func (g *terraformGuard) Description() string { return "Blocks access to Terraform credentials" }

func (g *terraformGuard) Rules(ctx *seatbelt.Context) seatbelt.GuardResult {
	result := seatbelt.GuardResult{}

	if cliConfig, ok := ctx.EnvLookup("TF_CLI_CONFIG_FILE"); ok && cliConfig != "" {
		if !pathExists(cliConfig) {
			result.Skipped = append(result.Skipped, fmt.Sprintf("%s not found", cliConfig))
			return result
		}
		result.Rules = append(result.Rules, seatbelt.SectionDeny("Terraform credentials"))
		result.Rules = append(result.Rules, DenyFile(cliConfig)...)
		result.Protected = append(result.Protected, cliConfig)
		result.Overrides = append(result.Overrides, seatbelt.Override{
			EnvVar:      "TF_CLI_CONFIG_FILE",
			Value:       cliConfig,
			DefaultPath: ctx.HomePath(".terraform.d/credentials.tfrc.json"),
		})
		return result
	}

	// Default credential locations
	credPath := ctx.HomePath(".terraform.d/credentials.tfrc.json")
	rcPath := ctx.HomePath(".terraformrc")

	if pathExists(credPath) {
		result.Rules = append(result.Rules, DenyFile(credPath)...)
		result.Protected = append(result.Protected, credPath)
	} else {
		result.Skipped = append(result.Skipped, fmt.Sprintf("%s not found", credPath))
	}

	if pathExists(rcPath) {
		result.Rules = append(result.Rules, DenyFile(rcPath)...)
		result.Protected = append(result.Protected, rcPath)
	} else {
		result.Skipped = append(result.Skipped, fmt.Sprintf("%s not found", rcPath))
	}

	if len(result.Rules) > 0 {
		result.Rules = append([]seatbelt.Rule{seatbelt.SectionDeny("Terraform credentials")}, result.Rules...)
	}

	return result
}
