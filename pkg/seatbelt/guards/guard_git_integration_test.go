package guards_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt"
	"github.com/jskswamy/aide/pkg/seatbelt/guards"
)

func TestGuard_GitIntegration_Metadata(t *testing.T) {
	g := guards.GitIntegrationGuard()
	if g.Name() != "git-integration" {
		t.Errorf("expected Name() = %q, got %q", "git-integration", g.Name())
	}
	if g.Type() != "always" {
		t.Errorf("expected Type() = %q, got %q", "always", g.Type())
	}
	if g.Description() == "" {
		t.Error("expected non-empty Description()")
	}
}

func resolveSymlinkOrSelf(p string) string {
	resolved, err := filepath.EvalSymlinks(p)
	if err != nil {
		return p
	}
	return resolved
}

func TestGuard_GitIntegration_WellKnownPaths(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".gitconfig"), []byte("[user]\n\tname = Test\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	g := guards.GitIntegrationGuard()
	ctx := &seatbelt.Context{HomeDir: home, ProjectRoot: "/project"}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	expectedPaths := []string{
		resolveSymlinkOrSelf(filepath.Join(home, ".gitconfig")),
		resolveSymlinkOrSelf(filepath.Join(home, ".config", "git", "config")),
		resolveSymlinkOrSelf(filepath.Join(home, ".config", "git", "ignore")),
		resolveSymlinkOrSelf(filepath.Join(home, ".config", "git", "attributes")),
		resolveSymlinkOrSelf(filepath.Join(home, ".gitignore")),
	}
	for _, p := range expectedPaths {
		if !strings.Contains(output, `"`+p+`"`) {
			t.Errorf("expected output to contain path %q", p)
		}
	}

	if !strings.Contains(output, "file-read*") {
		t.Error("expected file-read* rule")
	}
	if strings.Contains(output, "file-write*") {
		t.Error("git config paths should be read-only")
	}
}

func TestGuard_GitIntegration_CustomExcludes(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".gitconfig"), []byte("[core]\n\texcludesFile = ~/custom-ignores\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	g := guards.GitIntegrationGuard()
	ctx := &seatbelt.Context{HomeDir: home}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	customPath := resolveSymlinkOrSelf(filepath.Join(home, "custom-ignores"))
	if !strings.Contains(output, `"`+customPath+`"`) {
		t.Errorf("expected custom excludesFile path %q in output", customPath)
	}
}

func TestGuard_GitIntegration_EnvOverride(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	customConfig := filepath.Join(tmp, "custom-gitconfig")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(customConfig, []byte("[user]\n\tname = Custom\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	g := guards.GitIntegrationGuard()
	ctx := &seatbelt.Context{
		HomeDir: home,
		Env:     []string{"GIT_CONFIG_GLOBAL=" + customConfig},
	}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	if !strings.Contains(output, `"`+resolveSymlinkOrSelf(customConfig)+`"`) {
		t.Errorf("expected GIT_CONFIG_GLOBAL path %q in output", customConfig)
	}
	if len(result.Overrides) == 0 {
		t.Error("expected override for GIT_CONFIG_GLOBAL")
	}
}

func TestGuard_GitIntegration_GPGSigning(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(home, ".gitconfig"), []byte(
		"[commit]\n\tgpgsign = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	g := guards.GitIntegrationGuard()
	ctx := &seatbelt.Context{HomeDir: home, ProjectRoot: "/project"}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	// Should have GPG read-write access
	if !strings.Contains(output, ".gnupg") {
		t.Error("expected .gnupg path in rules when gpgsign is enabled")
	}
	if !strings.Contains(output, "file-write*") {
		t.Error("expected file-write* for .gnupg (GPG needs write access)")
	}
	// Should have GPG agent socket
	if !strings.Contains(output, "S.gpg-agent") {
		t.Error("expected GPG agent socket rule")
	}
}

func TestGuard_GitIntegration_NoGPGWhenNotConfigured(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(home, ".gitconfig"), []byte(
		"[user]\n\tname = Test\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	g := guards.GitIntegrationGuard()
	ctx := &seatbelt.Context{HomeDir: home}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	if strings.Contains(output, ".gnupg") {
		t.Error("should NOT have .gnupg rules when gpgsign is not enabled")
	}
}

func TestGuard_GitIntegration_SSHSigningKey(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(sshDir, "id_ed25519_signing")
	if err := os.WriteFile(keyPath, []byte("priv"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath+".pub", []byte("ssh-ed25519 AAAA..."), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(home, ".gitconfig"), []byte(
		"[commit]\n\tgpgsign = true\n[gpg]\n\tformat = ssh\n[user]\n\tsigningkey = "+keyPath+".pub\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	g := guards.GitIntegrationGuard()
	ctx := &seatbelt.Context{HomeDir: home}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	resolvedPriv := resolveSymlinkOrSelf(keyPath)
	resolvedPub := resolveSymlinkOrSelf(keyPath + ".pub")
	if !strings.Contains(output, `"`+resolvedPriv+`"`) {
		t.Errorf("expected private key path %q in rules", resolvedPriv)
	}
	if !strings.Contains(output, `"`+resolvedPub+`"`) {
		t.Errorf("expected .pub path %q in rules", resolvedPub)
	}
	knownHosts := resolveSymlinkOrSelf(filepath.Join(sshDir, "known_hosts"))
	if !strings.Contains(output, knownHosts) {
		t.Errorf("expected known_hosts path %q in rules", knownHosts)
	}
	// known_hosts is read-write (TOFU). The same rule must mention file-write*.
	if !strings.Contains(output, "file-write*") {
		t.Error("expected file-write* on known_hosts")
	}
}

func TestGuard_GitIntegration_SSHSigningKeyPrivatePath(t *testing.T) {
	// user.signingkey can point at the private key directly (without .pub).
	// We must still grant both the private and the .pub sibling.
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(sshDir, "id_ed25519_signing")
	if err := os.WriteFile(keyPath, []byte("priv"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(home, ".gitconfig"), []byte(
		"[gpg]\n\tformat = ssh\n[user]\n\tsigningkey = "+keyPath+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	g := guards.GitIntegrationGuard()
	ctx := &seatbelt.Context{HomeDir: home}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	resolvedPriv := resolveSymlinkOrSelf(keyPath)
	resolvedPub := resolveSymlinkOrSelf(filepath.Dir(keyPath)) + "/" + filepath.Base(keyPath) + ".pub"
	if !strings.Contains(output, `"`+resolvedPriv+`"`) {
		t.Errorf("expected private key path %q in rules", resolvedPriv)
	}
	if !strings.Contains(output, `"`+resolvedPub+`"`) {
		t.Errorf("expected derived .pub path %q in rules", resolvedPub)
	}
}

func TestGuard_GitIntegration_SSHSigningKeyLiteralPubkey(t *testing.T) {
	// user.signingkey = "ssh-ed25519 AAAA…" is the literal pubkey form (no file).
	// No file rule should be emitted; the case is recorded in Skipped.
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(home, ".gitconfig"), []byte(
		"[gpg]\n\tformat = ssh\n[user]\n\tsigningkey = ssh-ed25519 AAAAC3NzaC1lZDI1NTE5\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	g := guards.GitIntegrationGuard()
	ctx := &seatbelt.Context{HomeDir: home}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	// known_hosts rule should still be present.
	if !strings.Contains(output, "known_hosts") {
		t.Error("expected known_hosts rule even with literal pubkey signingkey")
	}
	// But no signing-key allow rule for any literal file path.
	if strings.Contains(output, "id_ed25519") {
		t.Error("literal pubkey form must not produce a signing key file rule")
	}
	// And a Skipped note should explain why.
	hasNote := false
	for _, s := range result.Skipped {
		if strings.Contains(s, "literal pubkey") {
			hasNote = true
		}
	}
	if !hasNote {
		t.Errorf("expected Skipped note about literal pubkey, got %v", result.Skipped)
	}
}

func TestGuard_GitIntegration_SSHSigningKeyEscapesHome(t *testing.T) {
	// T3/T4 defense in depth: even if signingkey somehow resolves outside HOME,
	// refuse to emit a rule for it. Production path (repo-local config blocked)
	// is covered separately; this guards against future regressions.
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(home, ".gitconfig"), []byte(
		"[gpg]\n\tformat = ssh\n[user]\n\tsigningkey = /etc/shadow.pub\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	g := guards.GitIntegrationGuard()
	ctx := &seatbelt.Context{HomeDir: home}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	if strings.Contains(output, "/etc/shadow") {
		t.Error("signingkey outside HOME must not produce a rule")
	}
	hasNote := false
	for _, s := range result.Skipped {
		if strings.Contains(s, "outside HOME") {
			hasNote = true
		}
	}
	if !hasNote {
		t.Errorf("expected Skipped note about HOME escape, got %v", result.Skipped)
	}
}

func TestGuard_GitIntegration_NoSSHRulesWhenFormatNotSSH(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".gitconfig"), []byte(
		"[commit]\n\tgpgsign = true\n[gpg]\n\tformat = openpgp\n[user]\n\tsigningkey = ABC123\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	g := guards.GitIntegrationGuard()
	ctx := &seatbelt.Context{HomeDir: home}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	if strings.Contains(output, "known_hosts") {
		t.Error("known_hosts rule must not appear when gpg.format != ssh")
	}
	if strings.Contains(output, "SSH commit signing key") {
		t.Error("SSH signing section must not appear when gpg.format != ssh")
	}
}

func TestGuard_GitIntegration_NilContext(t *testing.T) {
	g := guards.GitIntegrationGuard()
	result := g.Rules(nil)
	if len(result.Rules) != 0 {
		t.Error("expected no rules for nil context")
	}
}
