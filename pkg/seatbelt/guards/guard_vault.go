// Vault guard for macOS Seatbelt profiles.
//
// Protects HashiCorp Vault token files from leakage.

package guards

import "github.com/jskswamy/aide/pkg/seatbelt"

type vaultGuard struct{}

// VaultGuard returns a Guard that denies access to Vault token files.
func VaultGuard() seatbelt.Guard { return &vaultGuard{} }

func (g *vaultGuard) Name() string        { return "vault" }
func (g *vaultGuard) Type() string        { return "default" }
func (g *vaultGuard) Description() string { return "Blocks access to Vault token" }

func (g *vaultGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	tokenPath := EnvOverridePath(ctx, "VAULT_TOKEN_FILE", ".vault-token")

	var rules []seatbelt.Rule
	rules = append(rules, seatbelt.SectionRestrict("Vault credentials"))
	rules = append(rules, DenyFile(tokenPath)...)
	return rules
}
