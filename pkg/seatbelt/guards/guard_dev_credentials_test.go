package guards_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
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

func TestDevCredentials_GitCredentials(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	credFile := filepath.Join(home, ".git-credentials")
	if err := os.WriteFile(credFile, []byte("https://user:token@github.com"), 0o600); err != nil {
		t.Fatal(err)
	}

	g := guards.DevCredentialsGuard()
	ctx := &seatbelt.Context{HomeDir: home}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	if !strings.Contains(output, credFile) {
		t.Errorf("expected .git-credentials path %q in deny rules", credFile)
	}
	if !strings.Contains(output, "deny file-read-data") {
		t.Error("expected deny file-read-data rule for .git-credentials")
	}
}

func TestDevCredentials_NilContext(t *testing.T) {
	g := guards.DevCredentialsGuard()
	result := g.Rules(nil)
	if len(result.Rules) != 0 {
		t.Error("expected no rules for nil context")
	}
}

func TestDevCredentials_EmptyHomeDir(t *testing.T) {
	g := guards.DevCredentialsGuard()
	ctx := &seatbelt.Context{HomeDir: ""}
	result := g.Rules(ctx)
	if len(result.Rules) != 0 {
		t.Error("expected no rules for empty HomeDir")
	}
}

func TestDevCredentials_OptOutReadable(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}

	ghDir := filepath.Join(home, ".config", "gh")
	if err := os.MkdirAll(ghDir, 0o755); err != nil {
		t.Fatal(err)
	}

	g := guards.DevCredentialsGuard()
	// Opt out the gh directory via ExtraReadable
	ctx := &seatbelt.Context{
		HomeDir:       home,
		ExtraReadable: []string{ghDir},
	}
	result := g.Rules(ctx)

	// gh dir should be in Allowed, not Protected
	if !slices.Contains(result.Allowed, ghDir) {
		t.Errorf("expected %s in Allowed, got Allowed=%v", ghDir, result.Allowed)
	}
	// Should NOT have deny rules for gh dir
	output := renderTestRules(result.Rules)
	if strings.Contains(output, ghDir) {
		t.Errorf("opt-out path %s should not appear in deny rules", ghDir)
	}
}

func TestDevCredentials_OptOutWritable(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}

	credFile := filepath.Join(home, ".git-credentials")
	if err := os.WriteFile(credFile, []byte("token"), 0o600); err != nil {
		t.Fatal(err)
	}

	g := guards.DevCredentialsGuard()
	ctx := &seatbelt.Context{
		HomeDir:       home,
		ExtraWritable: []string{credFile},
	}
	result := g.Rules(ctx)

	if !slices.Contains(result.Allowed, credFile) {
		t.Errorf("expected %s in Allowed, got Allowed=%v", credFile, result.Allowed)
	}
}

func TestDevCredentials_DirNotFound(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	// Don't create any credential dirs or files

	g := guards.DevCredentialsGuard()
	ctx := &seatbelt.Context{HomeDir: home}
	result := g.Rules(ctx)

	// All paths should be skipped since nothing exists
	if len(result.Rules) != 0 {
		t.Errorf("expected no deny rules for missing paths, got %d", len(result.Rules))
	}
	if len(result.Skipped) == 0 {
		t.Error("expected skipped entries for missing paths")
	}
}

func TestDevCredentials_DirExistsFileDoesNot(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")

	// Create a dir credential path (.config/gh) but NOT a file credential path (.cargo/credentials.toml)
	ghDir := filepath.Join(home, ".config", "gh")
	if err := os.MkdirAll(ghDir, 0o755); err != nil {
		t.Fatal(err)
	}

	g := guards.DevCredentialsGuard()
	ctx := &seatbelt.Context{HomeDir: home}
	result := g.Rules(ctx)

	// gh dir should be protected
	output := renderTestRules(result.Rules)
	if !strings.Contains(output, ghDir) {
		t.Errorf("expected %s in deny rules", ghDir)
	}

	// .cargo/credentials.toml should be skipped
	cargoFile := filepath.Join(home, ".cargo/credentials.toml")
	skipped := fmt.Sprintf("%v", result.Skipped)
	if !strings.Contains(skipped, cargoFile) {
		t.Errorf("expected %s in skipped, got %v", cargoFile, result.Skipped)
	}
}

func TestDevCredentials_NameAndDescription(t *testing.T) {
	g := guards.DevCredentialsGuard()
	if g.Name() != "dev-credentials" {
		t.Errorf("expected Name() = %q, got %q", "dev-credentials", g.Name())
	}
	if g.Description() == "" {
		t.Error("expected non-empty Description()")
	}
}
