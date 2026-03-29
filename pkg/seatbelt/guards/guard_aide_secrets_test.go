package guards_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt"
	"github.com/jskswamy/aide/pkg/seatbelt/guards"
)

func TestAideSecrets_Metadata(t *testing.T) {
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

func TestAideSecrets_NilContext(t *testing.T) {
	g := guards.AideSecretsGuard()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked on nil context: %v", r)
		}
	}()
	result := g.Rules(nil)
	if len(result.Rules) != 0 {
		t.Error("expected no rules for nil context")
	}
}

func TestAideSecrets_EmptyHomeDir(t *testing.T) {
	g := guards.AideSecretsGuard()
	ctx := &seatbelt.Context{HomeDir: ""}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked on empty HomeDir: %v", r)
		}
	}()
	result := g.Rules(ctx)
	if len(result.Rules) != 0 {
		t.Error("expected no rules for empty HomeDir")
	}
}

func TestAideSecrets_SecretsExist(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	secretsDir := filepath.Join(home, ".config", "aide", "secrets")
	if err := os.MkdirAll(secretsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	g := guards.AideSecretsGuard()
	ctx := &seatbelt.Context{HomeDir: home}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	if !strings.Contains(output, "deny") {
		t.Error("expected deny rules when secrets directory exists")
	}
	if len(result.Protected) == 0 {
		t.Error("expected Protected to list secrets directory")
	}
}

func TestAideSecrets_SecretsMissing(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}

	g := guards.AideSecretsGuard()
	ctx := &seatbelt.Context{HomeDir: home}
	result := g.Rules(ctx)

	if len(result.Rules) != 0 {
		t.Error("expected no rules when secrets directory doesn't exist")
	}
	if len(result.Skipped) == 0 {
		t.Error("expected Skipped message when secrets directory doesn't exist")
	}
}
