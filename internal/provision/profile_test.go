package provision_test

import (
	"errors"
	"testing"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/provision"
	_ "github.com/jskswamy/aide/internal/provision/agents/claude"
	_ "github.com/jskswamy/aide/internal/provision/agents/codex"
	_ "github.com/jskswamy/aide/internal/provision/agents/copilot"
	_ "github.com/jskswamy/aide/internal/provision/agents/gemini"
)

func TestDriverBaseProfileDerivesPath(t *testing.T) {
	d := provision.DriverBase{Caps: provision.Capabilities{
		AgentName:     "claude",
		ProfileEnvKey: "CLAUDE_CONFIG_DIR",
	}}
	envKey, absPath, err := d.Profile("firmus", "", "/home/u")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if envKey != "CLAUDE_CONFIG_DIR" {
		t.Errorf("envKey = %q, want CLAUDE_CONFIG_DIR", envKey)
	}
	if absPath != "/home/u/.claude-firmus" {
		t.Errorf("absPath = %q, want /home/u/.claude-firmus", absPath)
	}
}

func TestDriverBaseProfileHonorsOverride(t *testing.T) {
	d := provision.DriverBase{Caps: provision.Capabilities{
		AgentName:     "claude",
		ProfileEnvKey: "CLAUDE_CONFIG_DIR",
	}}
	envKey, absPath, err := d.Profile("firmus", "~/custom-dir", "/home/u")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if envKey != "CLAUDE_CONFIG_DIR" {
		t.Errorf("envKey = %q", envKey)
	}
	if absPath != "/home/u/custom-dir" {
		t.Errorf("absPath = %q, want /home/u/custom-dir (tilde expanded)", absPath)
	}
}

func TestDriverBaseProfileOverrideAbsolutePath(t *testing.T) {
	d := provision.DriverBase{Caps: provision.Capabilities{
		AgentName:     "claude",
		ProfileEnvKey: "CLAUDE_CONFIG_DIR",
	}}
	_, absPath, err := d.Profile("foo", "/opt/claude-foo", "/home/u")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if absPath != "/opt/claude-foo" {
		t.Errorf("absPath = %q, want /opt/claude-foo (verbatim absolute)", absPath)
	}
}

func TestDriverBaseProfileUnsupportedAgent(t *testing.T) {
	d := provision.DriverBase{Caps: provision.Capabilities{
		AgentName:     "cursor-agent",
		ProfileEnvKey: "", // explicit non-support
	}}
	_, _, err := d.Profile("foo", "", "/home/u")
	if err == nil {
		t.Fatal("expected error for unsupported agent")
	}
	if !errors.Is(err, provision.ErrProfileNotSupported) {
		t.Errorf("error should wrap ErrProfileNotSupported, got %v", err)
	}
}

func TestValidateProfileAcceptsValidConfig(t *testing.T) {
	ctx := config.Context{Agent: "claude", Profile: "firmus"}
	if err := provision.ValidateProfile(ctx, "/home/u", false); err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestValidateProfileAcceptsEmpty(t *testing.T) {
	ctx := config.Context{Agent: "claude"}
	if err := provision.ValidateProfile(ctx, "/home/u", false); err != nil {
		t.Errorf("absent profile should be accepted, got %v", err)
	}
}

func TestValidateProfileRejectsBadName(t *testing.T) {
	for _, bad := range []string{"bad/name", "bad name", "../escape", ".hidden"} {
		ctx := config.Context{Agent: "claude", Profile: bad}
		err := provision.ValidateProfile(ctx, "/home/u", false)
		if err == nil {
			t.Errorf("expected rejection for %q", bad)
			continue
		}
		if !errors.Is(err, provision.ErrProfileNameInvalid) {
			t.Errorf("%q: expected ErrProfileNameInvalid, got %v", bad, err)
		}
	}
}

func TestValidateProfileRejectsDirWithoutProfile(t *testing.T) {
	ctx := config.Context{Agent: "claude", ProfileDir: "/some/dir"}
	err := provision.ValidateProfile(ctx, "/home/u", false)
	if !errors.Is(err, provision.ErrProfileNameInvalid) {
		t.Errorf("expected ErrProfileNameInvalid, got %v", err)
	}
}

func TestValidateProfileRejectsDirOutsideHome(t *testing.T) {
	ctx := config.Context{Agent: "claude", Profile: "foo", ProfileDir: "/etc/cursed"}
	err := provision.ValidateProfile(ctx, "/home/u", false)
	if !errors.Is(err, provision.ErrProfileDirOutsideHome) {
		t.Errorf("expected ErrProfileDirOutsideHome, got %v", err)
	}
}

func TestValidateProfileRejectsConflict(t *testing.T) {
	ctx := config.Context{
		Agent:   "claude",
		Profile: "foo",
		Env:     map[string]string{"CLAUDE_CONFIG_DIR": "/whatever"},
	}
	err := provision.ValidateProfile(ctx, "/home/u", false)
	if !errors.Is(err, provision.ErrProfileConflict) {
		t.Errorf("expected ErrProfileConflict, got %v", err)
	}
}

func TestValidateProfileRejectsFromProjectOverride(t *testing.T) {
	ctx := config.Context{Agent: "claude", Profile: "foo"}
	err := provision.ValidateProfile(ctx, "/home/u", true)
	if !errors.Is(err, provision.ErrProfileNotProjectScoped) {
		t.Errorf("expected ErrProfileNotProjectScoped, got %v", err)
	}
}

func TestResolveContextInjectsProfileEnv(t *testing.T) {
	ctx := config.Context{Agent: "claude", Profile: "firmus"}
	out, err := provision.ResolveContext("work", ctx, "/home/u", "/p", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, ok := out.Env["CLAUDE_CONFIG_DIR"]
	if !ok {
		t.Fatalf("CLAUDE_CONFIG_DIR not injected, env=%v", out.Env)
	}
	if got != "/home/u/.claude-firmus" {
		t.Errorf("CLAUDE_CONFIG_DIR = %q, want /home/u/.claude-firmus", got)
	}
}

func TestResolveContextLeavesEnvIntactWithoutProfile(t *testing.T) {
	ctx := config.Context{Agent: "claude", Env: map[string]string{"FOO": "bar"}}
	out, err := provision.ResolveContext("work", ctx, "/home/u", "/p", ctx.Env)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, ok := out.Env["CLAUDE_CONFIG_DIR"]; ok {
		t.Errorf("CLAUDE_CONFIG_DIR should not be injected, got %q", got)
	}
	if out.Env["FOO"] != "bar" {
		t.Errorf("existing env lost: %v", out.Env)
	}
}

// Per-driver registration sanity: each shipped driver populates
// ProfileEnvKey (or explicitly leaves it empty for cursor).
func TestShippedDriversProfileSupport(t *testing.T) {
	tests := []struct {
		agentName   string
		wantEnvKey  string
		wantSupport bool
	}{
		{"claude", "CLAUDE_CONFIG_DIR", true},
		{"gemini", "GEMINI_HOME", true},
		{"codex", "CODEX_HOME", true},
		{"copilot", "COPILOT_HOME", true},
	}
	for _, tc := range tests {
		t.Run(tc.agentName, func(t *testing.T) {
			drv, ok := provision.ProvisionerFor(tc.agentName)
			if !ok {
				t.Fatalf("provisioner not registered for %q", tc.agentName)
			}
			base, ok := drv.(interface {
				Profile(name, override, homeDir string) (string, string, error)
			})
			if !ok {
				t.Fatalf("driver %q does not expose Profile()", tc.agentName)
			}
			envKey, absPath, err := base.Profile("test", "", "/home/u")
			if err != nil {
				t.Fatalf("Profile() error: %v", err)
			}
			if envKey != tc.wantEnvKey {
				t.Errorf("envKey = %q, want %q", envKey, tc.wantEnvKey)
			}
			if absPath == "" {
				t.Errorf("absPath empty")
			}
		})
	}
}

func TestWithAgent_DropsPreviousAgentDirKeyWithoutMutatingCaller(t *testing.T) {
	env := map[string]string{"CLAUDE_CONFIG_DIR": "/elsewhere", "FOO": "bar"}
	ctx := config.Context{Agent: "claude", Profile: "work", Env: env}

	got := provision.WithAgent(ctx, "codex")

	if got.Agent != "codex" {
		t.Errorf("Agent = %q, want codex", got.Agent)
	}
	if _, ok := got.Env["CLAUDE_CONFIG_DIR"]; ok {
		t.Error("CLAUDE_CONFIG_DIR kept after switching away from claude")
	}
	if got.Env["FOO"] != "bar" {
		t.Errorf("FOO = %q, want bar", got.Env["FOO"])
	}
	if env["CLAUDE_CONFIG_DIR"] != "/elsewhere" {
		t.Error("caller's env map was mutated")
	}
}

func TestWithAgent_KeepsEnvWithoutProfile(t *testing.T) {
	ctx := config.Context{Agent: "claude", Env: map[string]string{"CLAUDE_CONFIG_DIR": "/x"}}
	if got := provision.WithAgent(ctx, "codex"); got.Env["CLAUDE_CONFIG_DIR"] != "/x" {
		t.Error("explicit env dropped for a context without profile:")
	}
}
