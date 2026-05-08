package guards_test

import (
	"strings"
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt"
	"github.com/jskswamy/aide/pkg/seatbelt/guards"
)

func TestGuard_SSH_Metadata(t *testing.T) {
	g := guards.SSHGuard()
	if g.Name() != "ssh" {
		t.Errorf("expected Name() = %q, got %q", "ssh", g.Name())
	}
	if g.Type() != "opt-in" {
		t.Errorf("expected Type() = %q, got %q", "opt-in", g.Type())
	}
	if g.Description() == "" {
		t.Error("expected non-empty Description()")
	}
}

func TestGuard_SSH_Rules_Defaults(t *testing.T) {
	g := guards.SSHGuard()
	ctx := &seatbelt.Context{
		HomeDir: "/Users/testuser",
		Env:     []string{"SSH_AUTH_SOCK=/tmp/ssh-agent.sock"},
	}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	if !strings.Contains(output, `"/Users/testuser/.ssh"`) {
		t.Error("expected ~/.ssh subpath read")
	}
	if !strings.Contains(output, "/tmp/ssh-agent.sock") {
		t.Error("expected SSH agent socket path")
	}
	if !strings.Contains(output, "unix-socket") {
		t.Error("expected unix-socket allow")
	}
	if !strings.Contains(output, `"*:22"`) {
		t.Error("expected default TCP *:22 allow")
	}

	// Env override for SSH_AUTH_SOCK passthrough
	foundSock := false
	for _, ov := range result.Overrides {
		if ov.EnvVar == "SSH_AUTH_SOCK" && ov.Value == "/tmp/ssh-agent.sock" {
			foundSock = true
		}
	}
	if !foundSock {
		t.Error("expected SSH_AUTH_SOCK override")
	}
}

func TestGuard_SSH_Rules_NoSSHAgent(t *testing.T) {
	g := guards.SSHGuard()
	ctx := &seatbelt.Context{HomeDir: "/Users/testuser"}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	if strings.Contains(output, "unix-socket") {
		t.Error("should not have agent socket rule when SSH_AUTH_SOCK is unset")
	}
	if !strings.Contains(output, `"*:22"`) {
		t.Error("expected default TCP *:22 allow even without agent socket")
	}
	if !strings.Contains(output, `"/Users/testuser/.ssh"`) {
		t.Error("expected ~/.ssh read even without agent socket")
	}
	if len(result.Skipped) == 0 {
		t.Error("expected skipped message for missing SSH_AUTH_SOCK")
	}
}

func TestGuard_SSH_Rules_NilContext(t *testing.T) {
	g := guards.SSHGuard()
	result := g.Rules(nil)
	if len(result.Rules) != 0 {
		t.Error("expected no rules for nil context")
	}
}

func TestGuard_SSH_RegisteredAsOptIn(t *testing.T) {
	g, ok := guards.GuardByName("ssh")
	if !ok {
		t.Fatal("expected ssh guard to be in the registry")
	}
	if g.Type() != "opt-in" {
		t.Errorf("ssh guard must be opt-in, got %q", g.Type())
	}
}

func TestGuard_SSH_Rules_NoPort443(t *testing.T) {
	g := guards.SSHGuard()
	ctx := &seatbelt.Context{HomeDir: "/Users/testuser"}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)
	if strings.Contains(output, `"*:443"`) {
		t.Error("ssh guard must not grant port 443 (HTTPS belongs to git-remote)")
	}
}
