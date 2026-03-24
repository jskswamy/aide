// Vault guard for macOS Seatbelt profiles.
//
// Protects HashiCorp Vault token files from leakage.

package guards

import (
	"fmt"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

type vaultGuard struct{}

// VaultGuard returns a Guard that denies access to Vault token files.
func VaultGuard() seatbelt.Guard { return &vaultGuard{} }

func (g *vaultGuard) Name() string        { return "vault" }
func (g *vaultGuard) Type() string        { return "default" }
func (g *vaultGuard) Description() string { return "Blocks access to Vault token" }

func (g *vaultGuard) Rules(ctx *seatbelt.Context) seatbelt.GuardResult {
	result := seatbelt.GuardResult{}
	tokenPath := EnvOverridePath(ctx, "VAULT_TOKEN_FILE", ".vault-token")

	if !pathExists(tokenPath) {
		result.Skipped = append(result.Skipped, fmt.Sprintf("%s not found", tokenPath))
		return result
	}

	// Check for env override
	if val, ok := ctx.EnvLookup("VAULT_TOKEN_FILE"); ok && val != "" {
		result.Overrides = append(result.Overrides, seatbelt.Override{
			EnvVar:      "VAULT_TOKEN_FILE",
			Value:       val,
			DefaultPath: ctx.HomePath(".vault-token"),
		})
	}

	result.Rules = append(result.Rules, seatbelt.SectionDeny("Vault credentials"))
	result.Rules = append(result.Rules, DenyFile(tokenPath)...)
	result.Protected = append(result.Protected, tokenPath)
	return result
}
