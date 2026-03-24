package guards_test

import (
	"os"
	"path/filepath"
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
	home := t.TempDir()
	// Create all the directories the guard checks for
	dirs := []string{
		".config/op",
		".op",
		".config/Bitwarden CLI",
		".password-store",
		".local/share/gopass",
		".gnupg/private-keys-v1.d",
	}
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(home, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// Create secring.gpg file
	if err := os.WriteFile(filepath.Join(home, ".gnupg/secring.gpg"), []byte("fake"), 0o600); err != nil {
		t.Fatal(err)
	}

	g := guards.PasswordManagersGuard()
	ctx := &seatbelt.Context{HomeDir: home}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	wantPaths := []string{
		".config/op",
		".op",
		".config/Bitwarden CLI",
		".password-store",
		".local/share/gopass",
		".gnupg",
	}
	for _, want := range wantPaths {
		if !strings.Contains(output, want) {
			t.Errorf("expected output to contain %q", want)
		}
	}

	if len(result.Protected) == 0 {
		t.Error("expected Protected to be populated")
	}
	if len(result.Skipped) != 0 {
		t.Errorf("expected no Skipped when all paths exist, got %v", result.Skipped)
	}
}

func TestGuard_PasswordManagers_AllSkipped(t *testing.T) {
	ctx := &seatbelt.Context{HomeDir: t.TempDir(), GOOS: "darwin"}
	result := guards.PasswordManagersGuard().Rules(ctx)
	if len(result.Rules) != 0 {
		t.Errorf("expected 0 rules when paths missing, got %d", len(result.Rules))
	}
	if len(result.Skipped) == 0 {
		t.Error("expected skip messages")
	}
}

func TestGuard_PasswordManagers_NoKeychain(t *testing.T) {
	// CRITICAL: Library/Keychains is managed by the keychain guard, not here
	home := t.TempDir()
	// Create all dirs so rules are generated
	for _, d := range []string{".config/op", ".op", ".config/Bitwarden CLI", ".password-store", ".local/share/gopass", ".gnupg/private-keys-v1.d"} {
		os.MkdirAll(filepath.Join(home, d), 0o755)
	}
	os.WriteFile(filepath.Join(home, ".gnupg/secring.gpg"), []byte("fake"), 0o600)

	g := guards.PasswordManagersGuard()
	ctx := &seatbelt.Context{HomeDir: home}
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
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, ".config/aide/secrets"), 0o755)

	g := guards.AideSecretsGuard()
	ctx := &seatbelt.Context{HomeDir: home}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	if !strings.Contains(output, filepath.Join(home, ".config/aide/secrets")) {
		t.Error("expected ~/.config/aide/secrets in output")
	}
	if len(result.Protected) == 0 {
		t.Error("expected Protected to be populated")
	}
}

func TestGuard_AideSecrets_AllSkipped(t *testing.T) {
	ctx := &seatbelt.Context{HomeDir: t.TempDir(), GOOS: "darwin"}
	result := guards.AideSecretsGuard().Rules(ctx)
	if len(result.Rules) != 0 {
		t.Errorf("expected 0 rules when path missing, got %d", len(result.Rules))
	}
	if len(result.Skipped) == 0 {
		t.Error("expected skip messages")
	}
}
