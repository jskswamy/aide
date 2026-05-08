package guards_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt"
	"github.com/jskswamy/aide/pkg/seatbelt/guards"
)

func writeGitConfig(t *testing.T, body string) string {
	t.Helper()
	root := t.TempDir()
	gitDir := filepath.Join(root, ".git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "config"), []byte(body), 0o644); err != nil {
		t.Fatalf("write .git/config: %v", err)
	}
	return root
}

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

func TestGuard_SSH_Ports_EnvOverride(t *testing.T) {
	g := guards.SSHGuard()
	ctx := &seatbelt.Context{
		HomeDir: "/Users/testuser",
		Env:     []string{"AIDE_SSH_PORTS=22,2222"},
	}
	output := renderTestRules(g.Rules(ctx).Rules)
	if !strings.Contains(output, `"*:22"`) {
		t.Error("expected *:22 from env override")
	}
	if !strings.Contains(output, `"*:2222"`) {
		t.Error("expected *:2222 from env override")
	}
}

func TestGuard_SSH_Ports_EnvOverride_OnlyCustom(t *testing.T) {
	g := guards.SSHGuard()
	ctx := &seatbelt.Context{
		HomeDir: "/Users/testuser",
		Env:     []string{"AIDE_SSH_PORTS=2222"},
	}
	output := renderTestRules(g.Rules(ctx).Rules)
	if !strings.Contains(output, `"*:2222"`) {
		t.Error("expected *:2222 from env override")
	}
	if strings.Contains(output, `"*:22"`) {
		t.Error("when AIDE_SSH_PORTS=2222 only, *:22 must not be granted (declared set wins)")
	}
}

func TestGuard_SSH_Ports_EnvOverride_InvalidIgnored(t *testing.T) {
	g := guards.SSHGuard()
	ctx := &seatbelt.Context{
		HomeDir: "/Users/testuser",
		Env:     []string{"AIDE_SSH_PORTS=22,abc,99999,0,2222"},
	}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)
	if !strings.Contains(output, `"*:22"`) {
		t.Error("expected valid *:22 to survive filter")
	}
	if !strings.Contains(output, `"*:2222"`) {
		t.Error("expected valid *:2222 to survive filter")
	}
	if strings.Contains(output, `"*:0"`) || strings.Contains(output, `"*:99999"`) {
		t.Error("invalid ports must be filtered out")
	}
	if strings.Contains(output, `"*:abc"`) {
		t.Error("non-numeric ports must be filtered out")
	}
	// Should log skip/info note about the bad entries
	if len(result.Skipped) == 0 {
		t.Error("expected Skipped note about ignored invalid AIDE_SSH_PORTS entries")
	}
}

func TestGuard_SSH_Ports_GitConfigSSHRemote(t *testing.T) {
	root := writeGitConfig(t, `
[remote "origin"]
	url = ssh://git@gitlab.example.com:2222/team/repo.git
[remote "secondary"]
	url = ssh://example.org:7022/repo.git
`)
	g := guards.SSHGuard()
	ctx := &seatbelt.Context{HomeDir: "/Users/testuser", ProjectRoot: root}
	output := renderTestRules(g.Rules(ctx).Rules)
	for _, want := range []string{`"*:22"`, `"*:2222"`, `"*:7022"`} {
		if !strings.Contains(output, want) {
			t.Errorf("expected %s from .git/config ssh:// remotes", want)
		}
	}
}

func TestGuard_SSH_Ports_GitConfigShorthandIgnored(t *testing.T) {
	// scp-style git@host:path doesn't carry an explicit port → only default 22.
	root := writeGitConfig(t, `
[remote "origin"]
	url = git@github.com:user/repo.git
`)
	g := guards.SSHGuard()
	ctx := &seatbelt.Context{HomeDir: "/Users/testuser", ProjectRoot: root}
	output := renderTestRules(g.Rules(ctx).Rules)
	if !strings.Contains(output, `"*:22"`) {
		t.Error("expected default *:22 for scp-style remotes")
	}
	if strings.Count(output, "*:") != 1 {
		t.Errorf("expected only the default port, got: %s", output)
	}
}

func TestGuard_SSH_Ports_FromSSHConfig(t *testing.T) {
	home := t.TempDir()
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatalf("mkdir .ssh: %v", err)
	}
	body := `
Host work
    HostName git.work.example
    Port 2222

Host alt
    HostName alt.example
    Port 7022
`
	if err := os.WriteFile(filepath.Join(sshDir, "config"), []byte(body), 0o600); err != nil {
		t.Fatalf("write ssh config: %v", err)
	}
	g := guards.SSHGuard()
	ctx := &seatbelt.Context{HomeDir: home}
	output := renderTestRules(g.Rules(ctx).Rules)
	for _, want := range []string{`"*:22"`, `"*:2222"`, `"*:7022"`} {
		if !strings.Contains(output, want) {
			t.Errorf("expected %s from ~/.ssh/config Host/Port", want)
		}
	}
}

func TestGuard_SSH_Ports_SSHConfig_HostlessIgnored(t *testing.T) {
	home := t.TempDir()
	sshDir := filepath.Join(home, ".ssh")
	_ = os.MkdirAll(sshDir, 0o700)
	// "Port 12345" outside any Host block: not associated with a host;
	// some ssh-config syntaxes allow it as a global default. We accept it.
	body := "Port 12345\n\nHost x\n    Port 4444\n"
	_ = os.WriteFile(filepath.Join(sshDir, "config"), []byte(body), 0o600)
	g := guards.SSHGuard()
	ctx := &seatbelt.Context{HomeDir: home}
	output := renderTestRules(g.Rules(ctx).Rules)
	for _, want := range []string{`"*:12345"`, `"*:4444"`} {
		if !strings.Contains(output, want) {
			t.Errorf("expected %s from ssh config", want)
		}
	}
}

func TestGuard_SSH_Ports_FromContextSSHPorts(t *testing.T) {
	g := guards.SSHGuard()
	ctx := &seatbelt.Context{
		HomeDir:  "/Users/testuser",
		SSHPorts: []int{22, 2222, 7022},
	}
	output := renderTestRules(g.Rules(ctx).Rules)
	for _, want := range []string{`"*:22"`, `"*:2222"`, `"*:7022"`} {
		if !strings.Contains(output, want) {
			t.Errorf("expected %s from ctx.SSHPorts (.aide.yaml capabilities.ssh.ports)", want)
		}
	}
}

func TestGuard_SSH_Ports_ContextOverridesAreUnioned(t *testing.T) {
	// .aide.yaml ports + auto-detect (~/.ssh/config) → union (no override semantics).
	home := t.TempDir()
	sshDir := filepath.Join(home, ".ssh")
	_ = os.MkdirAll(sshDir, 0o700)
	_ = os.WriteFile(filepath.Join(sshDir, "config"), []byte("Host w\n  Port 4444\n"), 0o600)

	g := guards.SSHGuard()
	ctx := &seatbelt.Context{HomeDir: home, SSHPorts: []int{2222}}
	output := renderTestRules(g.Rules(ctx).Rules)
	for _, want := range []string{`"*:22"`, `"*:2222"`, `"*:4444"`} {
		if !strings.Contains(output, want) {
			t.Errorf("expected %s in union of yaml + ssh-config", want)
		}
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
