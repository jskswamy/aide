// Aide secrets guard for macOS Seatbelt profiles.
//
// Protects aide's own secrets store from being read or written.

package guards

import (
	"fmt"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

type aideSecretsGuard struct{}

// AideSecretsGuard returns a Guard that denies access to aide's own secrets store.
func AideSecretsGuard() seatbelt.Guard { return &aideSecretsGuard{} }

func (g *aideSecretsGuard) Name() string        { return "aide-secrets" }
func (g *aideSecretsGuard) Type() string        { return "default" }
func (g *aideSecretsGuard) Description() string { return "Blocks access to aide's encrypted secrets" }

func (g *aideSecretsGuard) Rules(ctx *seatbelt.Context) seatbelt.GuardResult {
	result := seatbelt.GuardResult{}
	secretsDir := ctx.HomePath(".config/aide/secrets")

	if !dirExists(secretsDir) {
		result.Skipped = append(result.Skipped, fmt.Sprintf("%s not found", secretsDir))
		return result
	}

	result.Rules = append(result.Rules, seatbelt.SectionDeny("aide secrets"))
	result.Rules = append(result.Rules, DenyDir(secretsDir)...)
	result.Protected = append(result.Protected, secretsDir)
	return result
}
