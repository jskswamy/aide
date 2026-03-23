// Terraform guard for macOS Seatbelt profiles.
//
// Protects Terraform credential files from leakage.

package guards

import "github.com/jskswamy/aide/pkg/seatbelt"

type terraformGuard struct{}

// TerraformGuard returns a Guard that denies access to Terraform credential files.
func TerraformGuard() seatbelt.Guard { return &terraformGuard{} }

func (g *terraformGuard) Name() string        { return "terraform" }
func (g *terraformGuard) Type() string        { return "default" }
func (g *terraformGuard) Description() string { return "Blocks access to Terraform credentials" }

func (g *terraformGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	var rules []seatbelt.Rule
	rules = append(rules, seatbelt.SectionRestrict("Terraform credentials"))

	if cliConfig, ok := ctx.EnvLookup("TF_CLI_CONFIG_FILE"); ok && cliConfig != "" {
		rules = append(rules, DenyFile(cliConfig)...)
		return rules
	}

	// Default credential locations
	rules = append(rules, DenyFile(ctx.HomePath(".terraform.d/credentials.tfrc.json"))...)
	rules = append(rules, DenyFile(ctx.HomePath(".terraformrc"))...)
	return rules
}
