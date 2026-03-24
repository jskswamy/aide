// Password managers guard for macOS Seatbelt profiles.
//
// Protects password manager data directories from being read or written.
// Note: macOS Keychain (Library/Keychains) is managed by the keychain guard.

package guards

import (
	"fmt"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

type passwordManagersGuard struct{}

// PasswordManagersGuard returns a Guard that denies access to password manager data.
func PasswordManagersGuard() seatbelt.Guard { return &passwordManagersGuard{} }

func (g *passwordManagersGuard) Name() string        { return "password-managers" }
func (g *passwordManagersGuard) Type() string        { return "default" }
func (g *passwordManagersGuard) Description() string {
	return "Blocks access to password manager data and GPG private keys"
}

func (g *passwordManagersGuard) Rules(ctx *seatbelt.Context) seatbelt.GuardResult {
	result := seatbelt.GuardResult{}

	// 1Password CLI
	opDir := ctx.HomePath(".config/op")
	opLegacyDir := ctx.HomePath(".op")
	opFound := false
	if dirExists(opDir) {
		result.Rules = append(result.Rules, seatbelt.SectionDeny("1Password CLI"))
		result.Rules = append(result.Rules, DenyDir(opDir)...)
		result.Protected = append(result.Protected, opDir)
		opFound = true
	} else {
		result.Skipped = append(result.Skipped, fmt.Sprintf("%s not found", opDir))
	}
	if dirExists(opLegacyDir) {
		if !opFound {
			result.Rules = append(result.Rules, seatbelt.SectionDeny("1Password CLI"))
		}
		result.Rules = append(result.Rules, DenyDir(opLegacyDir)...)
		result.Protected = append(result.Protected, opLegacyDir)
	} else {
		result.Skipped = append(result.Skipped, fmt.Sprintf("%s not found", opLegacyDir))
	}

	// Bitwarden CLI
	bwDir := ctx.HomePath(".config/Bitwarden CLI")
	if dirExists(bwDir) {
		result.Rules = append(result.Rules, seatbelt.SectionDeny("Bitwarden CLI"))
		result.Rules = append(result.Rules, DenyDir(bwDir)...)
		result.Protected = append(result.Protected, bwDir)
	} else {
		result.Skipped = append(result.Skipped, fmt.Sprintf("%s not found", bwDir))
	}

	// pass (standard unix password manager)
	passDir := ctx.HomePath(".password-store")
	if dirExists(passDir) {
		result.Rules = append(result.Rules, seatbelt.SectionDeny("pass"))
		result.Rules = append(result.Rules, DenyDir(passDir)...)
		result.Protected = append(result.Protected, passDir)
	} else {
		result.Skipped = append(result.Skipped, fmt.Sprintf("%s not found", passDir))
	}

	// gopass
	gopassDir := ctx.HomePath(".local/share/gopass")
	if dirExists(gopassDir) {
		result.Rules = append(result.Rules, seatbelt.SectionDeny("gopass"))
		result.Rules = append(result.Rules, DenyDir(gopassDir)...)
		result.Protected = append(result.Protected, gopassDir)
	} else {
		result.Skipped = append(result.Skipped, fmt.Sprintf("%s not found", gopassDir))
	}

	// GPG private keys (used by pass/gopass and general signing)
	// Only block private key material -- public keyring and trustdb are
	// needed for GPG commit signing to work.
	gpgPrivDir := ctx.HomePath(".gnupg/private-keys-v1.d")
	gpgSecring := ctx.HomePath(".gnupg/secring.gpg")
	gpgFound := false
	if dirExists(gpgPrivDir) {
		result.Rules = append(result.Rules, seatbelt.SectionDeny("GPG private keys"))
		result.Rules = append(result.Rules, DenyDir(gpgPrivDir)...)
		result.Protected = append(result.Protected, gpgPrivDir)
		gpgFound = true
	} else {
		result.Skipped = append(result.Skipped, fmt.Sprintf("%s not found", gpgPrivDir))
	}
	if pathExists(gpgSecring) {
		if !gpgFound {
			result.Rules = append(result.Rules, seatbelt.SectionDeny("GPG private keys"))
		}
		result.Rules = append(result.Rules, DenyFile(gpgSecring)...)
		result.Protected = append(result.Protected, gpgSecring)
	} else {
		result.Skipped = append(result.Skipped, fmt.Sprintf("%s not found", gpgSecring))
	}

	return result
}

// --- aide-secrets ---

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
