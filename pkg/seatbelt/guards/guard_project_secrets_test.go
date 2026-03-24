package guards_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt"
	"github.com/jskswamy/aide/pkg/seatbelt/guards"
)

func TestProjectSecrets_DeniesEnvFiles(t *testing.T) {
	g := guards.ProjectSecretsGuard()

	if g.Type() != "default" {
		t.Errorf("expected type default, got %s", g.Type())
	}

	// Create a temp project with .env files
	projectDir := t.TempDir()
	os.WriteFile(filepath.Join(projectDir, ".env"), []byte("SECRET=foo"), 0644)
	os.WriteFile(filepath.Join(projectDir, ".env.local"), []byte("DB=bar"), 0644)
	os.WriteFile(filepath.Join(projectDir, ".envrc"), []byte("export X=1"), 0644)
	os.WriteFile(filepath.Join(projectDir, "main.go"), []byte("package main"), 0644)

	ctx := &seatbelt.Context{
		HomeDir:     "/Users/testuser",
		ProjectRoot: projectDir,
	}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	// Should deny .env, .env.local, .envrc but NOT main.go
	if !strings.Contains(output, ".env\"") {
		t.Error("expected .env to be denied")
	}
	if !strings.Contains(output, ".env.local") {
		t.Error("expected .env.local to be denied")
	}
	if !strings.Contains(output, ".envrc") {
		t.Error("expected .envrc to be denied")
	}
	if strings.Contains(output, "main.go") {
		t.Error("should NOT deny main.go")
	}
}

func TestProjectSecrets_DeniesGitHooksWrites(t *testing.T) {
	g := guards.ProjectSecretsGuard()

	projectDir := t.TempDir()
	gitHooksDir := filepath.Join(projectDir, ".git", "hooks")
	os.MkdirAll(gitHooksDir, 0755)

	ctx := &seatbelt.Context{
		HomeDir:     "/Users/testuser",
		ProjectRoot: projectDir,
	}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	if !strings.Contains(output, "hooks") {
		t.Error("expected .git/hooks write deny")
	}
	if !strings.Contains(output, "deny file-write*") {
		t.Error("expected deny file-write* for hooks")
	}
}

func TestProjectSecrets_SkipsNodeModules(t *testing.T) {
	g := guards.ProjectSecretsGuard()

	projectDir := t.TempDir()
	nmDir := filepath.Join(projectDir, "node_modules", "pkg")
	os.MkdirAll(nmDir, 0755)
	os.WriteFile(filepath.Join(nmDir, ".env"), []byte("LEAKED=1"), 0644)

	ctx := &seatbelt.Context{
		HomeDir:     "/Users/testuser",
		ProjectRoot: projectDir,
	}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	if strings.Contains(output, "node_modules") {
		t.Error("should NOT scan inside node_modules")
	}
}

func TestProjectSecrets_RespectsWritableExtra(t *testing.T) {
	g := guards.ProjectSecretsGuard()

	projectDir := t.TempDir()
	envFile := filepath.Join(projectDir, ".env")
	os.WriteFile(envFile, []byte("SECRET=foo"), 0644)

	ctx := &seatbelt.Context{
		HomeDir:       "/Users/testuser",
		ProjectRoot:   projectDir,
		ExtraWritable: []string{envFile},
	}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	if strings.Contains(output, ".env") {
		t.Error("should NOT deny .env when in ExtraWritable")
	}
}
