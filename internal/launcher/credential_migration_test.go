package launcher

import (
	"cmp"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/jskswamy/aide/internal/config"
	aidectx "github.com/jskswamy/aide/internal/context"
	"github.com/jskswamy/aide/internal/gatewaymigrate"
)

// Launch routes the same before a profile is migrated (the in-memory
// translation) and after (the written gateways): one gateway binding
// per provider credential, never the credential too.
func TestPlanProfileCredentials_SameBeforeAndAfterMigration(t *testing.T) {
	tests := []struct {
		name, harness, profile string
		want                   []string
	}{
		{"built-in anthropic for claude", "claude", `
secret: work
credentials:
  anthropic: {backend: age, store: work, key: anthropic_api_key}
default_credentials: [anthropic]
`, []string{"anthropic"}},
		{"built-in openai for pi", "pi", `
secret: work
credentials:
  openai: {backend: age, store: work, key: openai_api_key}
default_credentials: [openai]
`, []string{"openai"}},
		{"custom anthropic endpoint", "claude", `
secret: work
credentials:
  litellm:
    backend: age
    store: work
    key: llm
    for:
      claude: {env: [ANTHROPIC_AUTH_TOKEN], set: {ANTHROPIC_BASE_URL: "https://litellm.corp", ANTHROPIC_API_KEY: ""}}
default_credentials: [litellm]
`, []string{"litellm"}},
		{"bedrock binding", "claude", `
credentials:
  bedrock:
    capabilities: [aws]
    for:
      claude: {set: {CLAUDE_CODE_USE_BEDROCK: "1", AWS_REGION: us-east-1}}
default_credentials: [bedrock]
`, []string{"bedrock"}},
		{"env switch", "claude", `
env:
  CLAUDE_CODE_USE_VERTEX: "1"
  CLOUD_ML_REGION: us-east5
  ANTHROPIC_VERTEX_PROJECT_ID: acme-ml
`, []string{"vertex"}},
		{"gemini stays a credential", "gemini", `
secret: work
credentials:
  gemini: {backend: age, store: work, key: gemini_key}
default_credentials: [gemini]
`, []string{"gemini"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before, after, _ := planBoth(t, tt.harness, tt.profile)
			if !reflect.DeepEqual(before, after) {
				t.Errorf("before = %+v\nafter  = %+v", before, after)
			}
			var names []string
			for _, b := range before {
				names = append(names, b.Name)
			}
			if !reflect.DeepEqual(names, tt.want) {
				t.Errorf("bindings = %v, want %v", names, tt.want)
			}
		})
	}
}

// planBoth plans profile for harness before migration (the in-memory
// translation) and after (the written gateways).
func planBoth(t *testing.T, harness, profile string) (before, after []PlannedCredential, env [2]map[string]string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	cfgDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cfgDir, "profiles"), 0o700); err != nil {
		t.Fatal(err)
	}
	for f, body := range map[string]string{
		"config.yaml":        "default_profile: work\n",
		"profiles/work.yaml": "agent: " + harness + "\n" + profile,
	} {
		if err := os.WriteFile(filepath.Join(cfgDir, f), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	loaded, err := config.Load(cfgDir, "")
	if err != nil {
		t.Fatal(err)
	}
	plan, err := gatewaymigrate.PlanGatewayMigration(loaded, nil)
	if err != nil {
		t.Fatal(err)
	}
	var got [2][]PlannedCredential
	for i, cfg := range []*config.Config{loaded, plan.Config} {
		rc, err := aidectx.Resolve(cfg, t.TempDir(), nil)
		if err != nil {
			t.Fatal(err)
		}
		var sel Selection
		ctx, cp, err := PlanProfileCredentials(cfg, rc, SelectionInputs{}, &sel)
		if err != nil {
			t.Fatalf("PlanProfileCredentials() = %v", err)
		}
		got[i], env[i] = cp.Bindings, ctx.Env
	}
	return got[0], got[1], env
}

// What a launch sends where does not change with the migration: the key
// stays with the endpoint the profile set, in the header it used, and a
// profile env: keeps the variables no gateway carries. What no gateway
// can express exactly is refused when the config loads, never rerouted.
func TestPlanProfileCredentials_RoutingUnchanged(t *testing.T) {
	type route struct {
		name, store, key, url string
		env                   []string
	}
	tests := []struct {
		name, harness, profile string
		want                   []route
		wantEnv                map[string]string // left in env:
	}{
		{"env x-api-key to the vendor endpoint", "claude", `
secret: work
env:
  ANTHROPIC_BASE_URL: https://api.anthropic.com
  ANTHROPIC_API_KEY: "{{ .secrets.k }}"
`, []route{{"anthropic", "work", "k", "https://api.anthropic.com", []string{"ANTHROPIC_API_KEY"}}}, nil},
		{"env bearer key to a custom endpoint", "claude", `
secret: work
env:
  ANTHROPIC_BASE_URL: https://proxy.corp
  ANTHROPIC_AUTH_TOKEN: "{{ .secrets.k }}"
`, []route{{"anthropic_auth_token", "work", "k", "https://proxy.corp", []string{"ANTHROPIC_AUTH_TOKEN"}}}, nil},
		{"env switch keeps the region for pi", "pi", `
env:
  CLAUDE_CODE_USE_BEDROCK: "1"
  AWS_REGION: eu-west-1
`, nil, map[string]string{"AWS_REGION": "eu-west-1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before, after, env := planBoth(t, tt.harness, tt.profile)
			for i, got := range [][]PlannedCredential{before, after} {
				var routes []route
				for _, b := range got {
					routes = append(routes, route{b.Name, b.Store, b.Key, cmp.Or(b.Set["ANTHROPIC_BASE_URL"], b.Set["OPENAI_BASE_URL"]), slices.Clone(b.Env)})
				}
				if !reflect.DeepEqual(routes, tt.want) {
					t.Errorf("bindings (%d) = %+v, want %+v", i, routes, tt.want)
				}
				for k, v := range tt.wantEnv {
					if env[i][k] != v {
						t.Errorf("env (%d) lost %s = %q: %v", i, k, v, env[i])
					}
				}
			}
		})
	}
}
