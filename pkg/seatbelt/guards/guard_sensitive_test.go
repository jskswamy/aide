package guards_test

import (
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
	g := guards.DockerGuard()
	ctx := &seatbelt.Context{HomeDir: "/home/testuser"}
	output := renderTestRules(g.Rules(ctx))

	if !strings.Contains(output, "/home/testuser/.docker/config.json") {
		t.Error("expected ~/.docker/config.json in output")
	}
}

func TestGuard_Docker_EnvOverride(t *testing.T) {
	g := guards.DockerGuard()
	ctx := &seatbelt.Context{
		HomeDir: "/home/testuser",
		Env:     []string{"DOCKER_CONFIG=/custom/docker"},
	}
	output := renderTestRules(g.Rules(ctx))

	if !strings.Contains(output, "/custom/docker/config.json") {
		t.Error("expected DOCKER_CONFIG override path /custom/docker/config.json in output")
	}
	if strings.Contains(output, "/home/testuser/.docker/config.json") {
		t.Error("default docker config.json should not appear when DOCKER_CONFIG is set")
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
	g := guards.GithubCLIGuard()
	ctx := &seatbelt.Context{HomeDir: "/home/testuser"}
	output := renderTestRules(g.Rules(ctx))

	if !strings.Contains(output, "/home/testuser/.config/gh") {
		t.Error("expected ~/.config/gh in output")
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
	g := guards.NPMGuard()
	ctx := &seatbelt.Context{HomeDir: "/home/testuser"}
	output := renderTestRules(g.Rules(ctx))

	for _, want := range []string{
		"/home/testuser/.npmrc",
		"/home/testuser/.yarnrc",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("expected %q in output", want)
		}
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
	g := guards.NetrcGuard()
	ctx := &seatbelt.Context{HomeDir: "/home/testuser"}
	output := renderTestRules(g.Rules(ctx))

	if !strings.Contains(output, "/home/testuser/.netrc") {
		t.Error("expected ~/.netrc in output")
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
	g := guards.VercelGuard()
	ctx := &seatbelt.Context{HomeDir: "/home/testuser"}
	output := renderTestRules(g.Rules(ctx))

	if !strings.Contains(output, "/home/testuser/.config/vercel") {
		t.Error("expected ~/.config/vercel in output")
	}
}
