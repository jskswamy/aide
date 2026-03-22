// Vault guard for macOS Seatbelt profiles.
//
// Protects HashiCorp Vault token files from leakage.

package modules

import "github.com/jskswamy/aide/pkg/seatbelt"

type vaultGuard struct{}

// VaultGuard returns a Guard that denies access to Vault token files.
func VaultGuard() seatbelt.Guard { return &vaultGuard{} }

func (g *vaultGuard) Name() string        { return "vault" }
func (g *vaultGuard) Type() string        { return "default" }
func (g *vaultGuard) Description() string { return "HashiCorp Vault token file" }

func (g *vaultGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	tokenPath := envOverridePath(ctx, "VAULT_TOKEN_FILE", ".vault-token")

	var rules []seatbelt.Rule
	rules = append(rules, seatbelt.Section("Vault credentials"))
	rules = append(rules, denyLiteralRuleForPath(tokenPath)...)
	return rules
}
