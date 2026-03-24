package guards_test

import (
	"strings"
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt"
	"github.com/jskswamy/aide/pkg/seatbelt/guards"
)

func TestGuard_PasswordManagers_Metadata(t *testing.T) {
	g := guards.PasswordManagersGuard()
	if g.Name() != "password-managers" {
		t.Errorf("expected Name() = %q, got %q", "password-managers", g.Name())
	}
	if g.Type() != "default" {
		t.Errorf("expected Type() = %q, got %q", "default", g.Type())
	}
	if g.Description() == "" {
		t.Error("expected non-empty Description()")
	}
}

func TestGuard_PasswordManagers_AllPaths(t *testing.T) {
	g := guards.PasswordManagersGuard()
	ctx := &seatbelt.Context{HomeDir: "/home/testuser"}
	output := renderTestRules(g.Rules(ctx).Rules)

	wantPaths := []string{
		// 1Password CLI
		".config/op",
		".op",
		// Bitwarden CLI
		".config/Bitwarden CLI",
		// pass
		".password-store",
		// gopass
		".local/share/gopass",
		// GPG private keys
		".gnupg",
	}
	for _, want := range wantPaths {
		if !strings.Contains(output, want) {
			t.Errorf("expected output to contain %q", want)
		}
	}
}

func TestGuard_PasswordManagers_NoKeychain(t *testing.T) {
	// CRITICAL: Library/Keychains is managed by the keychain guard, not here
	g := guards.PasswordManagersGuard()
	ctx := &seatbelt.Context{HomeDir: "/home/testuser"}
	output := renderTestRules(g.Rules(ctx).Rules)

	if strings.Contains(output, "Library/Keychains") {
		t.Error("CRITICAL: password-managers guard must NOT contain Library/Keychains (managed by keychain guard)")
	}
}

func TestGuard_AideSecrets_Metadata(t *testing.T) {
	g := guards.AideSecretsGuard()
	if g.Name() != "aide-secrets" {
		t.Errorf("expected Name() = %q, got %q", "aide-secrets", g.Name())
	}
	if g.Type() != "default" {
		t.Errorf("expected Type() = %q, got %q", "default", g.Type())
	}
	if g.Description() == "" {
		t.Error("expected non-empty Description()")
	}
}

func TestGuard_AideSecrets_Path(t *testing.T) {
	g := guards.AideSecretsGuard()
	ctx := &seatbelt.Context{HomeDir: "/home/testuser"}
	output := renderTestRules(g.Rules(ctx).Rules)

	if !strings.Contains(output, "/home/testuser/.config/aide/secrets") {
		t.Error("expected ~/.config/aide/secrets in output")
	}
}
