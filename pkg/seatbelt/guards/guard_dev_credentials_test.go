package guards_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt"
	"github.com/jskswamy/aide/pkg/seatbelt/guards"
)

func TestDevCredentials_DeniesKnownCredFiles(t *testing.T) {
	g := guards.DevCredentialsGuard()

	if g.Type() != "default" {
		t.Errorf("expected type default, got %s", g.Type())
	}

	ctx := &seatbelt.Context{HomeDir: "/Users/testuser"}
	result := g.Rules(ctx)

	// Should have some combination of rules and skipped
	if len(result.Rules) == 0 && len(result.Skipped) == 0 {
		t.Error("expected either rules or skipped entries")
	}

	// Check that known cred paths are attempted
	output := renderTestRules(result.Rules)
	skipped := fmt.Sprintf("%v", result.Skipped)
	combined := output + skipped

	credPaths := []string{
		".config/gh",
		".cargo/credentials",
		".gradle/gradle.properties",
		".m2/settings.xml",
	}
	for _, p := range credPaths {
		if !strings.Contains(combined, p) {
			t.Errorf("expected %s to be protected or skipped", p)
		}
	}
}
