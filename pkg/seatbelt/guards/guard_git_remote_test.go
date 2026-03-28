package guards_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt"
	"github.com/jskswamy/aide/pkg/seatbelt/guards"
)

func TestGuard_GitRemote_Metadata(t *testing.T) {
	g := guards.GitRemoteGuard()
	if g.Name() != "git-remote" {
		t.Errorf("expected Name() = %q, got %q", "git-remote", g.Name())
	}
	if g.Type() != "opt-in" {
		t.Errorf("expected Type() = %q, got %q", "opt-in", g.Type())
	}
	if g.Description() == "" {
		t.Error("expected non-empty Description()")
	}
}

func TestGuard_GitRemote_Rules(t *testing.T) {
	g := guards.GitRemoteGuard()
	home := "/Users/testuser"
	ctx := &seatbelt.Context{
		HomeDir: home,
		Env:     []string{"SSH_AUTH_SOCK=/tmp/ssh-agent.sock"},
	}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	// SSH subpath
	if !strings.Contains(output, `"/Users/testuser/.ssh"`) {
		t.Error("expected SSH directory subpath")
	}

	// SSH agent socket
	if !strings.Contains(output, `/tmp/ssh-agent.sock`) {
		t.Error("expected SSH agent socket path")
	}
	if !strings.Contains(output, "network-outbound") {
		t.Error("expected network-outbound rule")
	}

	// Network ports
	if !strings.Contains(output, `"*:22"`) {
		t.Error("expected port 22 network rule")
	}
	if !strings.Contains(output, `"*:443"`) {
		t.Error("expected port 443 network rule")
	}

	// git-credentials deny
	gitCreds := filepath.Join(home, ".git-credentials")
	if !strings.Contains(output, gitCreds) {
		t.Error("expected .git-credentials deny rule")
	}
	if !strings.Contains(output, "deny file-read-data") {
		t.Error("expected deny file-read-data for git-credentials")
	}
}

func TestGuard_GitRemote_NoSSHAgent(t *testing.T) {
	g := guards.GitRemoteGuard()
	ctx := &seatbelt.Context{HomeDir: "/Users/testuser"}
	result := g.Rules(ctx)

	if len(result.Skipped) == 0 {
		t.Error("expected skipped message for missing SSH_AUTH_SOCK")
	}
	output := renderTestRules(result.Rules)
	if strings.Contains(output, "unix-socket") {
		t.Error("should not have SSH agent socket rule when SSH_AUTH_SOCK is unset")
	}
}

func TestGuard_GitRemote_NilContext(t *testing.T) {
	g := guards.GitRemoteGuard()
	result := g.Rules(nil)
	if len(result.Rules) != 0 {
		t.Error("expected no rules for nil context")
	}
}

func TestGuard_GitRemote_ReadOnly(t *testing.T) {
	g := guards.GitRemoteGuard()
	ctx := &seatbelt.Context{HomeDir: "/Users/testuser"}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	for line := range strings.SplitSeq(output, "\n") {
		if strings.Contains(line, "file-write*") && !strings.Contains(line, "deny") {
			t.Errorf("SSH paths should be read-only, found write rule: %s", line)
		}
	}
}
