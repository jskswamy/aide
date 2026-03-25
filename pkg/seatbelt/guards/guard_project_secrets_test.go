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

	projectDir := t.TempDir()
	for _, f := range []struct{ name, content string }{
		{".env", "SECRET=foo"},
		{".env.local", "DB=bar"},
		{".envrc", "export X=1"},
		{"main.go", "package main"},
	} {
		if err := os.WriteFile(filepath.Join(projectDir, f.name), []byte(f.content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	ctx := &seatbelt.Context{
		HomeDir:     "/Users/testuser",
		ProjectRoot: projectDir,
	}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

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
	if err := os.MkdirAll(gitHooksDir, 0755); err != nil {
		t.Fatal(err)
	}

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
	if err := os.MkdirAll(nmDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nmDir, ".env"), []byte("LEAKED=1"), 0644); err != nil {
		t.Fatal(err)
	}

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
	if err := os.WriteFile(envFile, []byte("SECRET=foo"), 0644); err != nil {
		t.Fatal(err)
	}

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
