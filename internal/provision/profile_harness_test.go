package provision_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/provision"
	_ "github.com/jskswamy/aide/internal/provision/agents/cursor"
	_ "github.com/jskswamy/aide/internal/provision/agents/opencode"
	_ "github.com/jskswamy/aide/internal/provision/agents/pi"
)

// Every harness without profile support, with or without a provision
// driver, reports the same error.
func TestProfileUnsupportedHarnesses(t *testing.T) {
	for _, agent := range []string{"cursor-agent", "cursor", "aider", "amp", "goose", "hermes", "my-custom-agent"} {
		t.Run(agent, func(t *testing.T) {
			ctx := config.Context{Agent: agent, Profile: "work"}
			want := fmt.Sprintf("Profiles are not supported for harness %q", agent)

			err := provision.ValidateProfile(ctx.Agent, ctx, "/home/u", false)
			if err == nil || err.Error() != want {
				t.Errorf("ValidateProfile error = %v, want %q", err, want)
			}
			if !errors.Is(err, provision.ErrProfileNotSupported) {
				t.Errorf("ValidateProfile error does not wrap ErrProfileNotSupported: %v", err)
			}

			_, err = provision.InjectProfileEnv(ctx.Agent, ctx, nil, "/home/u")
			if err == nil || err.Error() != want {
				t.Errorf("InjectProfileEnv error = %v, want %q", err, want)
			}
		})
	}
}

func TestInjectProfileEnvForPi(t *testing.T) {
	ctx := config.Context{Agent: "pi", Profile: "work"}
	env, err := provision.InjectProfileEnv(ctx.Agent, ctx, nil, "/home/u")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if env["PI_CODING_AGENT_DIR"] != "/home/u/.pi-work" {
		t.Errorf("PI_CODING_AGENT_DIR = %q, want /home/u/.pi-work", env["PI_CODING_AGENT_DIR"])
	}
}

func TestInjectProfileEnvForOpenCode(t *testing.T) {
	ctx := config.Context{Agent: "opencode", Profile: "work"}
	env, err := provision.InjectProfileEnv(ctx.Agent, ctx, nil, "/home/u")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if env["OPENCODE_CONFIG_DIR"] != "/home/u/.opencode-work" {
		t.Errorf("OPENCODE_CONFIG_DIR = %q, want /home/u/.opencode-work", env["OPENCODE_CONFIG_DIR"])
	}
	// Provision, preview and Skillshare also call InjectProfileEnv, so
	// the database path belongs to the launch alone.
	if v, ok := env["OPENCODE_DB"]; ok {
		t.Errorf("OPENCODE_DB = %q, want it unset", v)
	}
}

func TestProfileEnvKeyFor(t *testing.T) {
	tests := map[string]string{
		"claude":       "CLAUDE_CONFIG_DIR",
		"pi":           "PI_CODING_AGENT_DIR",
		"opencode":     "OPENCODE_CONFIG_DIR",
		"cursor-agent": "",
		"aider":        "",
		"unknown":      "",
	}
	for agent, want := range tests {
		if got := provision.ProfileEnvKeyFor(agent); got != want {
			t.Errorf("ProfileEnvKeyFor(%q) = %q, want %q", agent, got, want)
		}
	}
}

// The launcher and sandbox call the harness cursor-agent; its provision
// driver keeps the name cursor so existing state stays valid.
func TestProvisionerForResolvesHarnessAliases(t *testing.T) {
	for _, name := range []string{"cursor", "cursor-agent"} {
		drv, ok := provision.ProvisionerFor(name)
		if !ok {
			t.Fatalf("ProvisionerFor(%q) found nothing", name)
		}
		if drv.Name() != "cursor" {
			t.Errorf("ProvisionerFor(%q).Name() = %q, want cursor", name, drv.Name())
		}
	}
	if _, ok := provision.ProvisionerFor("aider"); ok {
		t.Error("ProvisionerFor(aider) found a driver")
	}
}
