package config

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func parseProfile(t *testing.T, body string) Context {
	t.Helper()
	var ctx Context
	if err := yaml.Unmarshal([]byte(body), &ctx); err != nil {
		t.Fatal(err)
	}
	return ctx
}

// gw is the want side of a converted gateway: only the fields the
// conversion decides.
type gw struct {
	preset, url, api, store, key, region, project string
	noAuth                                        bool
	harnesses                                     []string
}

func (g gw) matches(c GatewayConfig) bool {
	return c.Preset == g.preset && c.URL == g.url && c.API == g.api && c.Store == g.store && c.Key == g.key &&
		c.Region == g.region && c.Project == g.project && c.NoAuth == g.noAuth && slices.Equal(c.Harnesses, g.harnesses)
}

func TestConvertLegacyCredentials(t *testing.T) {
	tests := []struct {
		name     string
		yaml     string
		gateways map[string]gw
		choice   map[string]string
		creds    []string // removed from credentials:
		env      []string // removed from env:
		lost     []string // credentials that lost codex
		renames  map[string]map[string]string
	}{
		{
			name: "built-in anthropic becomes the anthropic preset on its default harnesses",
			yaml: `
secret: work
credentials:
  anthropic: {backend: age, store: work, key: anthropic_api_key}
default_credentials: [anthropic]
`,
			gateways: map[string]gw{"anthropic": {preset: "anthropic", store: "work", key: "anthropic_api_key", harnesses: []string{"claude", "pi", "omp", "opencode"}}},
			choice:   map[string]string{"claude": "anthropic", "pi": "anthropic", "omp": "anthropic", "opencode": "anthropic"},
			creds:    []string{"anthropic"},
		},
		{
			name: "not default-selected: gateway exists, choice is none",
			yaml: `
credentials:
  anthropic: {backend: age, store: work, key: k}
`,
			gateways: map[string]gw{"anthropic": {preset: "anthropic", store: "work", key: "k", harnesses: []string{"claude", "pi", "omp", "opencode"}}},
			choice:   map[string]string{"claude": "none", "pi": "none", "omp": "none", "opencode": "none"},
			creds:    []string{"anthropic"},
		},
		{
			name: "openrouter loses codex",
			yaml: `
credentials:
  openrouter: {backend: age, store: work, key: or_key}
default_credentials: [openrouter]
`,
			gateways: map[string]gw{"openrouter": {preset: "openrouter", store: "work", key: "or_key", harnesses: []string{"pi", "omp", "opencode"}}},
			choice:   map[string]string{"pi": "openrouter", "omp": "openrouter", "opencode": "openrouter"},
			creds:    []string{"openrouter"},
			lost:     []string{"openrouter"},
		},
		{
			name: "openai keeps codex and the listed harnesses only",
			yaml: `
credentials:
  openai: {backend: age, store: work, key: oa, harnesses: [pi, codex]}
default_credentials: [openai]
`,
			gateways: map[string]gw{"openai": {preset: "openai", store: "work", key: "oa", harnesses: []string{"pi", "codex"}}},
			choice:   map[string]string{"pi": "openai", "codex": "openai"},
			creds:    []string{"openai"},
		},
		{
			name: "codex credential maps to the codex preset",
			yaml: `
credentials:
  codex: {backend: age, store: work, key: cx}
default_credentials: [codex]
`,
			gateways: map[string]gw{"codex": {preset: "codex", store: "work", key: "cx", harnesses: []string{"codex"}}},
			choice:   map[string]string{"codex": "codex"},
			creds:    []string{"codex"},
		},
		{
			name: "custom OPENAI_BASE_URL splits codex into a responses sibling",
			yaml: `
credentials:
  corp:
    backend: age
    store: work
    key: corp_token
    for:
      pi: {env: [CORP_KEY], set: {OPENAI_BASE_URL: "https://llm.corp/v1"}}
      codex: {env: [CORP_KEY], set: {OPENAI_BASE_URL: "https://llm.corp/v1"}}
default_credentials: [corp]
`,
			gateways: map[string]gw{
				"corp":       {url: "https://llm.corp/v1", api: "openai-chat", store: "work", key: "corp_token", harnesses: []string{"pi"}},
				"corp-codex": {url: "https://llm.corp/v1", api: "openai-responses", store: "work", key: "corp_token", harnesses: []string{"codex"}},
			},
			choice:  map[string]string{"pi": "corp", "codex": "corp-codex"},
			creds:   []string{"corp"},
			renames: map[string]map[string]string{"codex": {"corp": "corp-codex"}},
		},
		{
			name: "custom OPENAI_BASE_URL without codex has no sibling",
			yaml: `
credentials:
  corp:
    backend: age
    store: work
    key: corp_token
    for:
      opencode: {env: [CORP_KEY], set: {OPENAI_BASE_URL: "https://llm.corp/v1"}}
`,
			gateways: map[string]gw{"corp": {url: "https://llm.corp/v1", api: "openai-chat", store: "work", key: "corp_token", harnesses: []string{"opencode"}}},
			choice:   map[string]string{"opencode": "none"},
			creds:    []string{"corp"},
		},
		{
			name: "custom ANTHROPIC_BASE_URL is an anthropic-messages gateway",
			yaml: `
credentials:
  litellm:
    backend: age
    store: work
    key: llm
    for:
      claude:
        env: [ANTHROPIC_AUTH_TOKEN]
        set: {ANTHROPIC_BASE_URL: "https://litellm.corp", ANTHROPIC_API_KEY: ""}
default_credentials: [litellm]
`,
			gateways: map[string]gw{"litellm": {url: "https://litellm.corp", api: "anthropic-messages", store: "work", key: "llm", harnesses: []string{"claude"}}},
			choice:   map[string]string{"claude": "litellm"},
			creds:    []string{"litellm"},
		},
		{
			name: "keyless loopback endpoint keeps no_auth",
			yaml: `
credentials:
  local:
    for:
      pi: {set: {OPENAI_BASE_URL: "http://127.0.0.1:4000/v1"}}
default_credentials: [local]
`,
			gateways: map[string]gw{"local": {url: "http://127.0.0.1:4000/v1", api: "openai-chat", noAuth: true, harnesses: []string{"pi"}}},
			choice:   map[string]string{"pi": "local"},
			creds:    []string{"local"},
		},
		{
			name: "bedrock binding becomes the bedrock preset",
			yaml: `
credentials:
  bedrock:
    capabilities: [aws]
    for:
      claude: {set: {CLAUDE_CODE_USE_BEDROCK: "1", AWS_REGION: us-east-1}}
default_credentials: [bedrock]
`,
			gateways: map[string]gw{"bedrock": {preset: "bedrock", region: "us-east-1", harnesses: []string{"claude"}}},
			choice:   map[string]string{"claude": "bedrock"},
			creds:    []string{"bedrock"},
		},
		{
			name: "vertex binding carries region and project",
			yaml: `
credentials:
  vertex:
    capabilities: [gcp]
    for:
      claude: {set: {CLAUDE_CODE_USE_VERTEX: "1", CLOUD_ML_REGION: us-east5, ANTHROPIC_VERTEX_PROJECT_ID: acme-ml}}
`,
			gateways: map[string]gw{"vertex": {preset: "vertex", region: "us-east5", project: "acme-ml", harnesses: []string{"claude"}}},
			choice:   map[string]string{"claude": "none"},
			creds:    []string{"vertex"},
		},
		{
			name: "env switch becomes a gateway and Claude's choice",
			yaml: `
env:
  CLAUDE_CODE_USE_BEDROCK: "1"
  AWS_REGION: eu-west-1
  EDITOR: vim
`,
			gateways: map[string]gw{"bedrock": {preset: "bedrock", region: "eu-west-1", harnesses: []string{"claude"}}},
			choice:   map[string]string{"claude": "bedrock"},
			env:      []string{"CLAUDE_CODE_USE_BEDROCK"}, // the region stays for the other harnesses
		},
		{
			name: "env endpoint with its key becomes a custom gateway",
			yaml: `
secret: work
env:
  ANTHROPIC_BASE_URL: https://proxy.corp
  ANTHROPIC_AUTH_TOKEN: "{{ .secrets.proxy_key }}"
`,
			gateways: map[string]gw{"anthropic_auth_token": {url: "https://proxy.corp", api: "anthropic-messages", store: "work", key: "proxy_key", harnesses: []string{"claude", "pi", "omp", "opencode"}}},
			choice:   map[string]string{"claude": "anthropic_auth_token", "pi": "anthropic_auth_token", "omp": "anthropic_auth_token", "opencode": "anthropic_auth_token"},
			env:      []string{"ANTHROPIC_BASE_URL", "ANTHROPIC_AUTH_TOKEN"},
		},
		{
			name: "env key alone becomes the preset",
			yaml: `
secret: work
env:
  OPENAI_API_KEY: "{{ .secrets.oa }}"
`,
			gateways: map[string]gw{"openai": {preset: "openai", store: "work", key: "oa", harnesses: []string{"pi", "omp", "opencode", "codex"}}},
			choice:   map[string]string{"pi": "openai", "omp": "openai", "opencode": "openai", "codex": "openai"},
			env:      []string{"OPENAI_API_KEY"},
		},
		{
			name: "env OPENAI_BASE_URL keeps its key unconverted",
			yaml: `
secret: work
env:
  OPENAI_BASE_URL: https://llm.corp/v1
  OPENAI_API_KEY: "{{ .secrets.oa }}"
`,
		},
		{
			name: "env templated ANTHROPIC_BASE_URL keeps its key unconverted",
			yaml: `
secret: work
env:
  ANTHROPIC_BASE_URL: "{{ .secrets.url }}"
  ANTHROPIC_API_KEY: "{{ .secrets.k }}"
`,
		},
		{
			name: "env x-api-key to a custom endpoint stays",
			yaml: `
secret: work
env:
  ANTHROPIC_BASE_URL: https://proxy.corp
  ANTHROPIC_API_KEY: "{{ .secrets.k }}"
`,
		},
		{
			name: "env x-api-key to the vendor URL becomes the anthropic preset",
			yaml: `
secret: work
env:
  ANTHROPIC_BASE_URL: https://api.anthropic.com/
  ANTHROPIC_API_KEY: "{{ .secrets.k }}"
`,
			gateways: map[string]gw{"anthropic": {preset: "anthropic", store: "work", key: "k", harnesses: []string{"claude", "pi", "omp", "opencode"}}},
			choice:   map[string]string{"claude": "anthropic", "pi": "anthropic", "omp": "anthropic", "opencode": "anthropic"},
			env:      []string{"ANTHROPIC_BASE_URL", "ANTHROPIC_API_KEY"},
		},
		{
			name: "x-api-key route to a custom endpoint stays",
			yaml: `
credentials:
  corp:
    backend: age
    store: work
    key: k
    for:
      claude: {env: [ANTHROPIC_API_KEY], set: {ANTHROPIC_BASE_URL: "https://proxy.corp"}}
`,
		},
		{
			name: "keyless claude endpoint stays so Claude keeps its login",
			yaml: `
credentials:
  local:
    for:
      claude: {set: {ANTHROPIC_BASE_URL: "http://127.0.0.1:4000"}}
`,
		},
		{
			name: "anthropic with codex loses codex without an add command",
			yaml: `
credentials:
  anthropic: {backend: age, store: work, key: k, harnesses: [claude, codex]}
`,
			gateways: map[string]gw{"anthropic": {preset: "anthropic", store: "work", key: "k", harnesses: []string{"claude"}}},
			choice:   map[string]string{"claude": "none"},
			creds:    []string{"anthropic"},
			lost:     []string{"anthropic"},
		},
		{
			name: "gemini and non-LLM credentials stay",
			yaml: `
credentials:
  gemini: {backend: age, store: work, key: g}
  github: {backend: age, store: work, key: gh, env: [GITHUB_TOKEN], harnesses: [claude]}
default_credentials: [gemini, github]
`,
		},
		{
			name: "a route that sets more than the endpoint is left alone",
			yaml: `
credentials:
  corp:
    backend: age
    store: work
    key: k
    for:
      claude: {env: [ANTHROPIC_AUTH_TOKEN], set: {ANTHROPIC_BASE_URL: "https://x.corp", ANTHROPIC_MODEL: opus}}
`,
		},
		{
			name: "an endpoint that would not load is left alone",
			yaml: `
credentials:
  corp:
    backend: age
    store: work
    key: k
    for:
      claude: {env: [ANTHROPIC_AUTH_TOKEN], set: {ANTHROPIC_BASE_URL: "http://remote.example.com"}}
`,
		},
		{
			name: "a reserved credential name is renamed",
			yaml: `
credentials:
  none:
    backend: age
    store: work
    key: k
    harnesses: [claude]
    for:
      claude: {env: [ANTHROPIC_AUTH_TOKEN], set: {ANTHROPIC_BASE_URL: "https://x.corp"}}
`,
			gateways: map[string]gw{"none-gw": {url: "https://x.corp", api: "anthropic-messages", store: "work", key: "k", harnesses: []string{"claude"}}},
			choice:   map[string]string{"claude": "none"},
			creds:    []string{"none"},
			renames:  map[string]map[string]string{"claude": {"none": "none-gw"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := parseProfile(t, tt.yaml)
			got := ConvertLegacy(ctx)
			for n, want := range tt.gateways {
				if g, ok := got.Gateways[n]; !ok || !want.matches(g) {
					t.Errorf("gateway %q = %+v, want %+v", n, got.Gateways[n], want)
				}
			}
			if len(got.Gateways) != len(tt.gateways) {
				t.Errorf("gateways = %v, want %d of them", got.Gateways, len(tt.gateways))
			}
			if tt.choice != nil && !reflect.DeepEqual(got.Choice, tt.choice) {
				t.Errorf("choice = %v, want %v", got.Choice, tt.choice)
			}
			if tt.choice == nil && len(got.Choice) != 0 {
				t.Errorf("choice = %v, want none", got.Choice)
			}
			if !reflect.DeepEqual(got.Credentials, tt.creds) || !sameSet(got.Env, tt.env) {
				t.Errorf("credentials = %v, env = %v; want %v, %v", got.Credentials, got.Env, tt.creds, tt.env)
			}
			if !reflect.DeepEqual(got.Renames, tt.renames) {
				t.Errorf("renames = %v, want %v", got.Renames, tt.renames)
			}
			var lost []string
			for _, l := range got.CodexLost {
				lost = append(lost, l.Name)
			}
			if !slices.Equal(lost, tt.lost) {
				t.Errorf("codex lost = %v, want %v", lost, tt.lost)
			}
			if want := len(tt.gateways) > 0 || len(tt.creds) > 0 || len(tt.env) > 0; got.NeedsMigration() != want {
				t.Errorf("NeedsMigration() = %v", got.NeedsMigration())
			}
		})
	}
}

func sameSet(a, b []string) bool {
	return slices.Equal(slices.Sorted(slices.Values(a)), slices.Sorted(slices.Values(b)))
}

// The residual keeps what launch must still inject through the
// credential path, and nothing that a gateway now injects.
func TestResidualDropsConvertedEntries(t *testing.T) {
	ctx := parseProfile(t, `
secret: work
env:
  CLAUDE_CODE_USE_BEDROCK: "1"
  AWS_REGION: us-east-1
  EDITOR: vim
credentials:
  openai: {backend: age, store: work, key: oa}
  gemini: {backend: age, store: work, key: g}
default_credentials: [openai, gemini]
`)
	got := ConvertLegacy(ctx).Apply(ctx).Residual()
	if _, ok := got.Credentials["openai"]; ok || got.Credentials["gemini"].Key != "g" {
		t.Errorf("credentials = %v", got.Credentials)
	}
	if !slices.Equal(got.DefaultCredentials, []string{"gemini"}) || !reflect.DeepEqual(got.Env, map[string]string{"EDITOR": "vim", "AWS_REGION": "us-east-1"}) {
		t.Errorf("default_credentials = %v, env = %v", got.DefaultCredentials, got.Env)
	}
	if ctx.Env["AWS_REGION"] == "" || len(ctx.Credentials) != 2 {
		t.Error("Residual changed its receiver")
	}
}

// A profile converted once loads clean: nothing is left to convert.
func TestConvertLegacyIsIdempotentOnMigratedProfile(t *testing.T) {
	cfg, err := loadYAML(t, `default_profile: work
profiles:
  work:
    agent: claude
    secret: work
    credentials:
      gemini: {backend: age, store: work, key: g}
    gateways:
      anthropic: {preset: anthropic, store: work, key: anthropic_api_key, harnesses: [claude]}
    gateway_choice: {claude: anthropic}
`)
	if err != nil {
		t.Fatalf("a gateway keyed like a provider variable must load: %v", err)
	}
	if cfg.NeedsGatewayMigration || strings.Contains(strings.Join(cfg.Contexts["work"].DefaultCredentials, ","), "anthropic") {
		t.Error("a migrated profile needs no migration")
	}
}
