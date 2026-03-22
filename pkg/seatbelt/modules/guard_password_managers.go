// Password managers guard for macOS Seatbelt profiles.
//
// Protects password manager data directories from being read or written.
// Note: macOS Keychain (Library/Keychains) is managed by the keychain guard.

package modules

import "github.com/jskswamy/aide/pkg/seatbelt"

type passwordManagersGuard struct{}

// PasswordManagersGuard returns a Guard that denies access to password manager data.
func PasswordManagersGuard() seatbelt.Guard { return &passwordManagersGuard{} }

func (g *passwordManagersGuard) Name() string        { return "password-managers" }
func (g *passwordManagersGuard) Type() string        { return "default" }
func (g *passwordManagersGuard) Description() string { return "1Password, Bitwarden, pass, gopass, and GPG private keys" }

func (g *passwordManagersGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	var rules []seatbelt.Rule

	// 1Password CLI
	rules = append(rules, seatbelt.Section("1Password CLI"))
	rules = append(rules, denySubpathRuleForPath(ctx.HomePath(".config/op"))...)
	rules = append(rules, denySubpathRuleForPath(ctx.HomePath(".op"))...)

	// Bitwarden CLI
	rules = append(rules, seatbelt.Section("Bitwarden CLI"))
	rules = append(rules, denySubpathRuleForPath(ctx.HomePath(".config/Bitwarden CLI"))...)

	// pass (standard unix password manager)
	rules = append(rules, seatbelt.Section("pass"))
	rules = append(rules, denySubpathRuleForPath(ctx.HomePath(".password-store"))...)

	// gopass
	rules = append(rules, seatbelt.Section("gopass"))
	rules = append(rules, denySubpathRuleForPath(ctx.HomePath(".local/share/gopass"))...)

	// GPG private keys (used by pass/gopass and general signing)
	rules = append(rules, seatbelt.Section("GPG private keys"))
	rules = append(rules, denySubpathRuleForPath(ctx.HomePath(".gnupg"))...)

	return rules
}

// --- aide-secrets ---

type aideSecretsGuard struct{}

// AideSecretsGuard returns a Guard that denies access to aide's own secrets store.
func AideSecretsGuard() seatbelt.Guard { return &aideSecretsGuard{} }

func (g *aideSecretsGuard) Name() string        { return "aide-secrets" }
func (g *aideSecretsGuard) Type() string        { return "default" }
func (g *aideSecretsGuard) Description() string { return "aide secrets store (~/.config/aide/secrets)" }

func (g *aideSecretsGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	var rules []seatbelt.Rule
	rules = append(rules, seatbelt.Section("aide secrets"))
	rules = append(rules, denySubpathRuleForPath(ctx.HomePath(".config/aide/secrets"))...)
	return rules
}
