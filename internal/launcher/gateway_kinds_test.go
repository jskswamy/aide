package launcher

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/config"
	aidectx "github.com/jskswamy/aide/internal/context"
)

func TestGatewayBindings_XAPIKeyKeyTravelsAsAPIKey(t *testing.T) {
	ctx := config.Context{Gateways: map[string]config.GatewayConfig{
		"anth": {Preset: "anthropic", Store: "s", Key: "k", Harnesses: []string{"claude"}},
	}}
	plan := gatewayBindings(ctx, "claude", Choice{Value: "anth"})
	env, err := ResolveCredentialEnv(CredentialPlan{Bindings: plan}, func(string) (map[string]string, error) {
		return map[string]string{"k": "sk-ant-secret"}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if env["ANTHROPIC_API_KEY"] != "sk-ant-secret" || env["ANTHROPIC_AUTH_TOKEN"] != "" ||
		env["ANTHROPIC_BASE_URL"] != "https://api.anthropic.com" {
		t.Errorf("env = %v", env)
	}
	// The key still may not ride along with another binding's endpoint.
	other := PlannedCredential{Name: "proxy", Set: map[string]string{"ANTHROPIC_BASE_URL": "https://proxy.example.com"}}
	if err := checkKeyStaysWithEndpoint("work", append(plan, other)); err == nil {
		t.Error("checkKeyStaysWithEndpoint accepted an x-api-key gateway next to another endpoint")
	}
}

const switchClaudeConfig = `
profiles:
  work:
    agent: claude
    gateways:
      aws:
        preset: bedrock
        region: eu-west-1
    gateway_choice:
      claude: aws
default_profile: work
`

func TestLauncher_ProviderSwitchLaunch(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTHROPIC_API_KEY", "ambient-key")
	t.Setenv("AWS_WEB_IDENTITY_TOKEN_FILE", "/var/run/token")
	prev := loadSecretStore
	loadSecretStore = func(string) (map[string]string, error) {
		t.Error("a provider switch decrypts nothing")
		return nil, nil
	}
	t.Cleanup(func() { loadSecretStore = prev })

	env, stderr, err := launchCredentialsIn(t, home, switchClaudeConfig, "", "", true)
	if err != nil {
		t.Fatalf("Launch() error = %v\n%s", err, stderr)
	}
	for k, want := range map[string]string{"CLAUDE_CODE_USE_BEDROCK": "1", "AWS_REGION": "eu-west-1", "AWS_WEB_IDENTITY_TOKEN_FILE": "/var/run/token"} {
		if got, _ := envValue(env, k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	if got, ok := envValue(env, "ANTHROPIC_API_KEY"); ok && got != "" {
		t.Errorf("ambient ANTHROPIC_API_KEY reached the agent")
	}
	if strings.Contains(stderr, "fetching") || strings.Contains(stderr, "warning") {
		t.Errorf("stderr:\n%s", stderr)
	}
	if entries, _ := os.ReadDir(filepath.Join(home, ".claude")); len(entries) > 0 {
		t.Errorf("a switch wrote harness files: %v", entries)
	}
}

func TestPlanProfileCredentials_ProviderSwitchOpensItsCapability(t *testing.T) {
	rc := &aidectx.ResolvedContext{Name: "work", Context: config.Context{
		Agent:         "claude",
		GatewayChoice: map[string]string{"claude": "aws"},
		ConvertedGateways: map[string]config.GatewayConfig{
			"aws": {Preset: "bedrock", Region: "eu-west-1"},
		},
	}}
	ctx, plan, err := PlanProfileCredentials(&config.Config{}, rc, SelectionInputs{}, &Selection{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Bindings) != 1 || plan.Bindings[0].Store != "" || plan.Bindings[0].Set["AWS_REGION"] != "eu-west-1" {
		t.Fatalf("bindings = %+v", plan.Bindings)
	}
	if !slices.Contains(ctx.Capabilities, "aws") {
		t.Errorf("capabilities = %v, want aws", ctx.Capabilities)
	}
}
