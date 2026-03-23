// Password managers guard for macOS Seatbelt profiles.
//
// Protects password manager data directories from being read or written.
// Note: macOS Keychain (Library/Keychains) is managed by the keychain guard.

package guards

import "github.com/jskswamy/aide/pkg/seatbelt"

type passwordManagersGuard struct{}

// PasswordManagersGuard returns a Guard that denies access to password manager data.
func PasswordManagersGuard() seatbelt.Guard { return &passwordManagersGuard{} }

func (g *passwordManagersGuard) Name() string        { return "password-managers" }
func (g *passwordManagersGuard) Type() string        { return "default" }
func (g *passwordManagersGuard) Description() string {
	return "Blocks access to password manager data and GPG private keys"
}

func (g *passwordManagersGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	var rules []seatbelt.Rule

	// 1Password CLI
	rules = append(rules, seatbelt.SectionRestrict("1Password CLI"))
	rules = append(rules, DenyDir(ctx.HomePath(".config/op"))...)
	rules = append(rules, DenyDir(ctx.HomePath(".op"))...)

	// Bitwarden CLI
	rules = append(rules, seatbelt.SectionRestrict("Bitwarden CLI"))
	rules = append(rules, DenyDir(ctx.HomePath(".config/Bitwarden CLI"))...)

	// pass (standard unix password manager)
	rules = append(rules, seatbelt.SectionRestrict("pass"))
	rules = append(rules, DenyDir(ctx.HomePath(".password-store"))...)

	// gopass
	rules = append(rules, seatbelt.SectionRestrict("gopass"))
	rules = append(rules, DenyDir(ctx.HomePath(".local/share/gopass"))...)

	// GPG private keys (used by pass/gopass and general signing)
	// Only block private key material — public keyring and trustdb are
	// needed for GPG commit signing to work.
	rules = append(rules, seatbelt.SectionRestrict("GPG private keys"))
	rules = append(rules, DenyDir(ctx.HomePath(".gnupg/private-keys-v1.d"))...)
	rules = append(rules, DenyFile(ctx.HomePath(".gnupg/secring.gpg"))...)

	return rules
}

// --- aide-secrets ---

type aideSecretsGuard struct{}

// AideSecretsGuard returns a Guard that denies access to aide's own secrets store.
func AideSecretsGuard() seatbelt.Guard { return &aideSecretsGuard{} }

func (g *aideSecretsGuard) Name() string        { return "aide-secrets" }
func (g *aideSecretsGuard) Type() string        { return "default" }
func (g *aideSecretsGuard) Description() string { return "Blocks access to aide's encrypted secrets" }

func (g *aideSecretsGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	var rules []seatbelt.Rule
	rules = append(rules, seatbelt.SectionRestrict("aide secrets"))
	rules = append(rules, DenyDir(ctx.HomePath(".config/aide/secrets"))...)
	return rules
}
