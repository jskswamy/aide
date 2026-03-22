package modules_test

import (
	"strings"
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt"
	"github.com/jskswamy/aide/pkg/seatbelt/modules"
)

func TestGuard_SSHKeys_Metadata(t *testing.T) {
	g := modules.SSHKeysGuard()

	if g.Name() != "ssh-keys" {
		t.Errorf("expected Name() = %q, got %q", "ssh-keys", g.Name())
	}
	if g.Type() != "default" {
		t.Errorf("expected Type() = %q, got %q", "default", g.Type())
	}
	if g.Description() == "" {
		t.Error("expected non-empty Description()")
	}
}

func TestGuard_SSHKeys_DenyRulesUseSubpath(t *testing.T) {
	g := modules.SSHKeysGuard()
	ctx := &seatbelt.Context{HomeDir: "/home/testuser"}
	output := renderTestRules(g.Rules(ctx))

	// .ssh directory should be denied via subpath
	if !strings.Contains(output, `(deny file-read-data`) {
		t.Error("expected deny file-read-data rule")
	}
	if !strings.Contains(output, `(deny file-write*`) {
		t.Error("expected deny file-write* rule")
	}
	if !strings.Contains(output, `(subpath "/home/testuser/.ssh")`) {
		t.Error("expected subpath for ~/.ssh in deny rules")
	}
}

func TestGuard_SSHKeys_AllowRulesUseLiteral(t *testing.T) {
	g := modules.SSHKeysGuard()
	ctx := &seatbelt.Context{HomeDir: "/home/testuser"}
	output := renderTestRules(g.Rules(ctx))

	// known_hosts and config should be allowed via literal
	if !strings.Contains(output, `(allow file-read*`) {
		t.Error("expected allow file-read* rule for known_hosts and config")
	}
	if !strings.Contains(output, `(literal "/home/testuser/.ssh/known_hosts")`) {
		t.Error("expected literal allow for ~/.ssh/known_hosts")
	}
	if !strings.Contains(output, `(literal "/home/testuser/.ssh/config")`) {
		t.Error("expected literal allow for ~/.ssh/config")
	}
}

func TestGuard_SSHKeys_MetadataAllowPresent(t *testing.T) {
	g := modules.SSHKeysGuard()
	ctx := &seatbelt.Context{HomeDir: "/home/testuser"}
	output := renderTestRules(g.Rules(ctx))

	// directory listing via file-read-metadata literal
	if !strings.Contains(output, `(allow file-read-metadata`) {
		t.Error("expected allow file-read-metadata rule for ~/.ssh directory")
	}
	if !strings.Contains(output, `(literal "/home/testuser/.ssh")`) {
		t.Error("expected literal for ~/.ssh in metadata allow rule")
	}
}
