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

func TestGuard_GitIntegration_NilContext(t *testing.T) {
	g := guards.GitIntegrationGuard()
	result := g.Rules(nil)
	if len(result.Rules) != 0 {
		t.Error("expected no rules for nil context")
	}
}
