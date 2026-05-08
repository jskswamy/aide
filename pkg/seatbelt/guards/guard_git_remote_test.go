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

func TestGuard_GitRemote_HTTPSAndCredentialsDeny(t *testing.T) {
	g := guards.GitRemoteGuard()
	home := "/Users/testuser"
	ctx := &seatbelt.Context{HomeDir: home}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	if !strings.Contains(output, "network-outbound") {
		t.Error("expected network-outbound rule")
	}
	if !strings.Contains(output, `"*:443"`) {
		t.Error("expected port 443 network rule")
	}

	gitCreds := filepath.Join(home, ".git-credentials")
	if !strings.Contains(output, gitCreds) {
		t.Error("expected .git-credentials deny rule")
	}
	if !strings.Contains(output, "deny file-read-data") {
		t.Error("expected deny file-read-data for git-credentials")
	}
}

func TestGuard_GitRemote_HintsOnSSHRemote(t *testing.T) {
	root := writeGitConfig(t, `
[remote "origin"]
	url = git@github.com:user/repo.git
[remote "gitlab"]
	url = ssh://git@gitlab.example.com:2222/team/repo.git
`)
	g := guards.GitRemoteGuard()
	ctx := &seatbelt.Context{HomeDir: "/Users/testuser", ProjectRoot: root}
	result := g.Rules(ctx)

	if len(result.Hints) == 0 {
		t.Fatal("expected a hint when .git/config has ssh-style remotes")
	}
	joined := strings.Join(result.Hints, "\n")
	if !strings.Contains(joined, "ssh") {
		t.Errorf("hint should reference 'ssh' capability; got %q", joined)
	}
}

func TestGuard_GitRemote_NoHintsForHTTPSOnly(t *testing.T) {
	root := writeGitConfig(t, `
[remote "origin"]
	url = https://github.com/user/repo.git
`)
	g := guards.GitRemoteGuard()
	ctx := &seatbelt.Context{HomeDir: "/Users/testuser", ProjectRoot: root}
	result := g.Rules(ctx)

	if len(result.Hints) != 0 {
		t.Errorf("expected no SSH hints when all remotes are HTTPS, got: %v", result.Hints)
	}
}

func TestGuard_GitRemote_NilContext(t *testing.T) {
	g := guards.GitRemoteGuard()
	result := g.Rules(nil)
	if len(result.Rules) != 0 {
		t.Error("expected no rules for nil context")
	}
}

func TestGuard_GitRemote_HTTPSOnly_NoSSHPrimitives(t *testing.T) {
	g := guards.GitRemoteGuard()
	ctx := &seatbelt.Context{
		HomeDir: "/Users/testuser",
		Env:     []string{"SSH_AUTH_SOCK=/tmp/ssh-agent.sock"},
	}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	if !strings.Contains(output, `"*:443"`) {
		t.Error("git-remote should still grant *:443 (HTTPS)")
	}
	if strings.Contains(output, `"*:22"`) {
		t.Error("git-remote must NOT grant *:22 — moved to ssh capability")
	}
	if strings.Contains(output, "unix-socket") {
		t.Error("git-remote must NOT grant SSH agent socket — moved to ssh capability")
	}
	if strings.Contains(output, "/Users/testuser/.ssh") {
		t.Error("git-remote must NOT grant ~/.ssh read — moved to ssh capability")
	}
	for _, ov := range result.Overrides {
		if ov.EnvVar == "SSH_AUTH_SOCK" {
			t.Error("git-remote must NOT pass SSH_AUTH_SOCK — moved to ssh capability")
		}
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
