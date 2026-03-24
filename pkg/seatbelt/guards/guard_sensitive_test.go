package guards_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt"
	"github.com/jskswamy/aide/pkg/seatbelt/guards"
)

// --- docker ---

func TestGuard_Docker_Metadata(t *testing.T) {
	g := guards.DockerGuard()
	if g.Name() != "docker" {
		t.Errorf("expected Name() = %q, got %q", "docker", g.Name())
	}
	if g.Type() != "opt-in" {
		t.Errorf("expected Type() = %q, got %q", "opt-in", g.Type())
	}
	if g.Description() == "" {
		t.Error("expected non-empty Description()")
	}
}

func TestGuard_Docker_DefaultPath(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, ".docker"), 0o755)
	os.WriteFile(filepath.Join(home, ".docker/config.json"), []byte("fake"), 0o600)

	g := guards.DockerGuard()
	ctx := &seatbelt.Context{HomeDir: home}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	if !strings.Contains(output, filepath.Join(home, ".docker/config.json")) {
		t.Error("expected ~/.docker/config.json in output")
	}
	if len(result.Protected) == 0 {
		t.Error("expected Protected to be populated")
	}
}

func TestGuard_Docker_AllSkipped(t *testing.T) {
	ctx := &seatbelt.Context{HomeDir: t.TempDir(), GOOS: "darwin"}
	result := guards.DockerGuard().Rules(ctx)
	if len(result.Rules) != 0 {
		t.Errorf("expected 0 rules when paths missing, got %d", len(result.Rules))
	}
	if len(result.Skipped) == 0 {
		t.Error("expected skip messages")
	}
}

func TestGuard_Docker_EnvOverride(t *testing.T) {
	home := t.TempDir()
	customDocker := filepath.Join(home, "custom-docker")
	os.MkdirAll(customDocker, 0o755)
	os.WriteFile(filepath.Join(customDocker, "config.json"), []byte("fake"), 0o600)

	g := guards.DockerGuard()
	ctx := &seatbelt.Context{
		HomeDir: home,
		Env:     []string{"DOCKER_CONFIG=" + customDocker},
	}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	if !strings.Contains(output, filepath.Join(customDocker, "config.json")) {
		t.Error("expected DOCKER_CONFIG override path in output")
	}
	if strings.Contains(output, filepath.Join(home, ".docker/config.json")) {
		t.Error("default docker config.json should not appear when DOCKER_CONFIG is set")
	}
	if len(result.Overrides) == 0 {
		t.Error("expected Overrides to be populated for DOCKER_CONFIG")
	}
}

// --- github-cli ---

func TestGuard_GithubCLI_Metadata(t *testing.T) {
	g := guards.GithubCLIGuard()
	if g.Name() != "github-cli" {
		t.Errorf("expected Name() = %q, got %q", "github-cli", g.Name())
	}
	if g.Type() != "opt-in" {
		t.Errorf("expected Type() = %q, got %q", "opt-in", g.Type())
	}
}

func TestGuard_GithubCLI_Path(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, ".config/gh"), 0o755)

	g := guards.GithubCLIGuard()
	ctx := &seatbelt.Context{HomeDir: home}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	if !strings.Contains(output, filepath.Join(home, ".config/gh")) {
		t.Error("expected ~/.config/gh in output")
	}
	if len(result.Protected) == 0 {
		t.Error("expected Protected to be populated")
	}
}

func TestGuard_GithubCLI_AllSkipped(t *testing.T) {
	ctx := &seatbelt.Context{HomeDir: t.TempDir(), GOOS: "darwin"}
	result := guards.GithubCLIGuard().Rules(ctx)
	if len(result.Rules) != 0 {
		t.Errorf("expected 0 rules when paths missing, got %d", len(result.Rules))
	}
	if len(result.Skipped) == 0 {
		t.Error("expected skip messages")
	}
}

// --- npm ---

func TestGuard_NPM_Metadata(t *testing.T) {
	g := guards.NPMGuard()
	if g.Name() != "npm" {
		t.Errorf("expected Name() = %q, got %q", "npm", g.Name())
	}
	if g.Type() != "opt-in" {
		t.Errorf("expected Type() = %q, got %q", "opt-in", g.Type())
	}
}

func TestGuard_NPM_Paths(t *testing.T) {
	home := t.TempDir()
	os.WriteFile(filepath.Join(home, ".npmrc"), []byte("fake"), 0o600)
	os.WriteFile(filepath.Join(home, ".yarnrc"), []byte("fake"), 0o600)

	g := guards.NPMGuard()
	ctx := &seatbelt.Context{HomeDir: home}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	for _, want := range []string{
		filepath.Join(home, ".npmrc"),
		filepath.Join(home, ".yarnrc"),
	} {
		if !strings.Contains(output, want) {
			t.Errorf("expected %q in output", want)
		}
	}
	if len(result.Protected) == 0 {
		t.Error("expected Protected to be populated")
	}
}

func TestGuard_NPM_AllSkipped(t *testing.T) {
	ctx := &seatbelt.Context{HomeDir: t.TempDir(), GOOS: "darwin"}
	result := guards.NPMGuard().Rules(ctx)
	if len(result.Rules) != 0 {
		t.Errorf("expected 0 rules when paths missing, got %d", len(result.Rules))
	}
	if len(result.Skipped) == 0 {
		t.Error("expected skip messages")
	}
}

// --- netrc ---

func TestGuard_Netrc_Metadata(t *testing.T) {
	g := guards.NetrcGuard()
	if g.Name() != "netrc" {
		t.Errorf("expected Name() = %q, got %q", "netrc", g.Name())
	}
	if g.Type() != "opt-in" {
		t.Errorf("expected Type() = %q, got %q", "opt-in", g.Type())
	}
}

func TestGuard_Netrc_Path(t *testing.T) {
	home := t.TempDir()
	os.WriteFile(filepath.Join(home, ".netrc"), []byte("fake"), 0o600)

	g := guards.NetrcGuard()
	ctx := &seatbelt.Context{HomeDir: home}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	if !strings.Contains(output, filepath.Join(home, ".netrc")) {
		t.Error("expected ~/.netrc in output")
	}
	if len(result.Protected) == 0 {
		t.Error("expected Protected to be populated")
	}
}

func TestGuard_Netrc_AllSkipped(t *testing.T) {
	ctx := &seatbelt.Context{HomeDir: t.TempDir(), GOOS: "darwin"}
	result := guards.NetrcGuard().Rules(ctx)
	if len(result.Rules) != 0 {
		t.Errorf("expected 0 rules when paths missing, got %d", len(result.Rules))
	}
	if len(result.Skipped) == 0 {
		t.Error("expected skip messages")
	}
}

// --- vercel ---

func TestGuard_Vercel_Metadata(t *testing.T) {
	g := guards.VercelGuard()
	if g.Name() != "vercel" {
		t.Errorf("expected Name() = %q, got %q", "vercel", g.Name())
	}
	if g.Type() != "opt-in" {
		t.Errorf("expected Type() = %q, got %q", "opt-in", g.Type())
	}
}

func TestGuard_Vercel_Path(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, ".config/vercel"), 0o755)

	g := guards.VercelGuard()
	ctx := &seatbelt.Context{HomeDir: home}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	if !strings.Contains(output, filepath.Join(home, ".config/vercel")) {
		t.Error("expected ~/.config/vercel in output")
	}
	if len(result.Protected) == 0 {
		t.Error("expected Protected to be populated")
	}
}

func TestGuard_Vercel_AllSkipped(t *testing.T) {
	ctx := &seatbelt.Context{HomeDir: t.TempDir(), GOOS: "darwin"}
	result := guards.VercelGuard().Rules(ctx)
	if len(result.Rules) != 0 {
		t.Errorf("expected 0 rules when paths missing, got %d", len(result.Rules))
	}
	if len(result.Skipped) == 0 {
		t.Error("expected skip messages")
	}
}
