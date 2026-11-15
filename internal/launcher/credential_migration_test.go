package launcher

import (
	"os"
	"path/filepath"
	"reflect"
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
			t.Setenv("HOME", t.TempDir())
			cfgDir := t.TempDir()
			if err := os.MkdirAll(filepath.Join(cfgDir, "profiles"), 0o700); err != nil {
				t.Fatal(err)
			}
			for f, body := range map[string]string{
				"config.yaml":        "default_profile: work\n",
				"profiles/work.yaml": "agent: " + tt.harness + "\n" + tt.profile,
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
				_, _, cp, err := PlanProfileCredentials(cfg, rc, SelectionInputs{}, &sel)
				if err != nil {
					t.Fatalf("PlanProfileCredentials() = %v", err)
				}
				got[i] = cp.Bindings
			}
			if !reflect.DeepEqual(got[0], got[1]) {
				t.Errorf("before = %+v\nafter  = %+v", got[0], got[1])
			}
			var names []string
			for _, b := range got[0] {
				names = append(names, b.Name)
			}
			if !reflect.DeepEqual(names, tt.want) {
				t.Errorf("bindings = %v, want %v", names, tt.want)
			}
		})
	}
}
