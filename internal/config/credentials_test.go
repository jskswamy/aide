package config

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const credentialProfiles = `profiles:
  work:
    agent: claude
    secret: work
    credentials:
      anthropic:
        backend: age
        store: work
        key: anthropic_api_key
      corp:
        backend: age
        store: corp
        key: corp_token
        env: [CORP_LLM_TOKEN]
        harnesses: [pi]
    default_credentials: [anthropic]
default_profile: work
`

func TestLoad_CredentialBindings(t *testing.T) {
	cfg, err := loadYAML(t, credentialProfiles)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	ctx := cfg.Contexts["work"]
	want := CredentialBinding{Backend: "age", Store: "work", Key: "anthropic_api_key"}
	if got := ctx.Credentials["anthropic"]; !equalBinding(got, want) {
		t.Errorf("credentials.anthropic = %+v, want %+v", got, want)
	}
	corp := ctx.Credentials["corp"]
	if !slices.Equal(corp.Env, []string{"CORP_LLM_TOKEN"}) || !slices.Equal(corp.Harnesses, []string{"pi"}) {
		t.Errorf("credentials.corp = %+v", corp)
	}
	if !slices.Equal(ctx.DefaultCredentials, []string{"anthropic"}) {
		t.Errorf("default_credentials = %v, want [anthropic]", ctx.DefaultCredentials)
	}

	out, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	var back Config
	if err := yaml.Unmarshal(out, &back); err != nil {
		t.Fatalf("re-reading output: %v\n%s", err, out)
	}
	if got := back.Contexts["work"]; len(got.Credentials) != 2 || !slices.Equal(got.DefaultCredentials, []string{"anthropic"}) {
		t.Errorf("round trip lost credentials:\n%s", out)
	}
}

func equalBinding(a, b CredentialBinding) bool {
	return a.Backend == b.Backend && a.Store == b.Store && a.Key == b.Key &&
		slices.Equal(a.Env, b.Env) && slices.Equal(a.Harnesses, b.Harnesses)
}

func TestLoad_InvalidCredentialsFail(t *testing.T) {
	cases := map[string]struct{ entry, want string }{
		"other backend":     {"credentials: {anthropic: {backend: vault, store: w, key: k}}", "backend"},
		"missing backend":   {"credentials: {anthropic: {store: w, key: k}}", "backend"},
		"missing store":     {"credentials: {anthropic: {backend: age, key: k}}", "store"},
		"missing key":       {"credentials: {anthropic: {backend: age, store: w}}", "key"},
		"custom no env":     {"credentials: {corp: {backend: age, store: w, key: k, harnesses: [pi]}}", "env"},
		"custom no harn":    {"credentials: {corp: {backend: age, store: w, key: k, env: [CORP_KEY]}}", "harnesses"},
		"unknown harness":   {"credentials: {anthropic: {backend: age, store: w, key: k, harnesses: [nosuch]}}", "nosuch"},
		"bad env name":      {"credentials: {anthropic: {backend: age, store: w, key: k, env: [\"BAD-NAME\"]}}", "BAD-NAME"},
		"secret material":   {"credentials: {anthropic: {backend: age, store: w, key: k, env: [SOPS_AGE_KEY]}}", "SOPS_AGE_KEY"},
		"sops family":       {"credentials: {anthropic: {backend: age, store: w, key: k, env: [SOPS_AGE_KEY_CMD]}}", "SOPS_AGE_KEY_CMD"},
		"reserved PATH":     {"credentials: {anthropic: {backend: age, store: w, key: k, env: [PATH]}}", "PATH"},
		"harness dir key":   {"credentials: {anthropic: {backend: age, store: w, key: k, env: [CLAUDE_CONFIG_DIR]}}", "CLAUDE_CONFIG_DIR"},
		"aide variable":     {"credentials: {anthropic: {backend: age, store: w, key: k, env: [AIDE_PROFILE]}}", "AIDE_PROFILE"},
		"undefined default": {"default_credentials: [openrouter]", "openrouter"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := loadYAML(t, "profiles:\n  work:\n    agent: claude\n    "+tc.entry+"\n")
			if !errors.Is(err, ErrInvalidCredentials) {
				t.Fatalf("Load() error = %v, want ErrInvalidCredentials", err)
			}
			if !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "work") {
				t.Errorf("error %q should name profile work and %s", err, tc.want)
			}
		})
	}
}

func TestCredentialBinding_EnvNamesAndHarnesses(t *testing.T) {
	builtin := CredentialBinding{Backend: "age", Store: "w", Key: "k"}
	if got := builtin.EnvNames("openrouter"); !slices.Equal(got, []string{"OPENROUTER_API_KEY"}) {
		t.Errorf("openrouter env = %v", got)
	}
	if got := builtin.EnvNames("anthropic"); !slices.Equal(got, []string{"ANTHROPIC_API_KEY"}) {
		t.Errorf("anthropic env = %v", got)
	}
	override := CredentialBinding{Env: []string{"MY_KEY"}}
	if got := override.EnvNames("anthropic"); !slices.Equal(got, []string{"MY_KEY"}) {
		t.Errorf("override env = %v, want [MY_KEY]", got)
	}
	if !builtin.AllowsHarness("codex") {
		t.Error("a built-in without harnesses: should allow every harness")
	}
	restricted := CredentialBinding{Harnesses: []string{"pi"}}
	if !restricted.AllowsHarness("pi") || restricted.AllowsHarness("claude") {
		t.Error("harnesses: [pi] should allow only pi")
	}
	if !restricted.AllowsHarness("/opt/bin/pi") {
		t.Error("an agent path should match by its harness name")
	}
}

func TestConfig_HarnessOfResolvesAgentAliases(t *testing.T) {
	cfg := &Config{Agents: map[string]AgentDef{
		"my-claude": {Binary: "/opt/bin/claude"},
		"claude":    {Binary: "pi"},
		"wrapper":   {Binary: "/usr/bin/some-wrapper"},
	}}
	for agent, want := range map[string]string{
		"my-claude":    "claude",
		"claude":       "pi",
		"wrapper":      "wrapper",
		"codex":        "codex",
		"/usr/bin/pi":  "pi",
		"unknown-tool": "unknown-tool",
	} {
		if got := cfg.HarnessOf(agent); got != want {
			t.Errorf("HarnessOf(%q) = %q, want %q", agent, got, want)
		}
	}
}

func TestConfig_CredentialScrubNames(t *testing.T) {
	cfg, err := loadYAML(t, credentialProfiles+"scrub_env: [EXTRA_*]\n")
	if err != nil {
		t.Fatal(err)
	}
	got := cfg.CredentialScrubNames()
	for _, w := range []string{"EXTRA_*", "CORP_LLM_TOKEN", "ANTHROPIC_API_KEY"} {
		if !slices.Contains(got, w) {
			t.Errorf("CredentialScrubNames() = %v, missing %s", got, w)
		}
	}
}

func TestTranslateLegacyCredentials_SecretTemplatesBecomeBindings(t *testing.T) {
	ctx := Context{
		Agent:              "claude",
		Secret:             "work",
		Env:                map[string]string{"ANTHROPIC_API_KEY": "{{ .secrets.anthropic_api_key }}", "ANTHROPIC_AUTH_TOKEN": `{{ index .secrets "tok" }}`, "FOO": "bar"},
		DefaultCredentials: []string{},
	}
	got, legacy, err := TranslateLegacyCredentials(ctx, "work")
	if err != nil {
		t.Fatalf("TranslateLegacyCredentials() error = %v", err)
	}
	if _, ok := got.Env["ANTHROPIC_API_KEY"]; ok {
		t.Error("translated entry still in env:")
	}
	if got.Env["FOO"] != "bar" {
		t.Errorf("unrelated env lost: %v", got.Env)
	}
	if _, ok := ctx.Env["ANTHROPIC_API_KEY"]; !ok {
		t.Error("translation mutated the caller's env map")
	}
	b, ok := got.Credentials["anthropic"]
	if !ok || b.Store != "work" || b.Key != "anthropic_api_key" || b.Backend != "age" {
		t.Errorf("anthropic binding = %+v (ok=%v)", b, ok)
	}
	tok, ok := got.Credentials["anthropic_auth_token"]
	if !ok || tok.Key != "tok" || !slices.Equal(tok.Env, []string{"ANTHROPIC_AUTH_TOKEN"}) {
		t.Errorf("anthropic_auth_token binding = %+v (ok=%v)", tok, ok)
	}
	if !slices.Equal(got.DefaultCredentials, []string{"anthropic", "anthropic_auth_token"}) {
		t.Errorf("DefaultCredentials = %v, want the implicit bindings selected", got.DefaultCredentials)
	}
	if len(legacy) != 2 || legacy[0].EnvVar != "ANTHROPIC_API_KEY" || legacy[0].Name != "anthropic" {
		t.Errorf("legacy = %+v", legacy)
	}
	hint := legacy[0].Hint("work")
	for _, w := range []string{"ANTHROPIC_API_KEY", "credentials:", "anthropic:", "store: work", "key: anthropic_api_key", "default_credentials"} {
		if !strings.Contains(hint, w) {
			t.Errorf("hint lacks %q:\n%s", w, hint)
		}
	}
}

func TestTranslateLegacyCredentials_SelectorsAreOrdinaryEnv(t *testing.T) {
	ctx := Context{Agent: "claude", Env: map[string]string{
		"CLAUDE_CODE_USE_BEDROCK": "1",
		"OPENAI_BASE_URL":         "https://proxy.example",
	}}
	got, legacy, err := TranslateLegacyCredentials(ctx, "")
	if err != nil {
		t.Fatalf("selectors in env: should not stop the launch: %v", err)
	}
	if len(legacy) != 0 || got.Env["CLAUDE_CODE_USE_BEDROCK"] != "1" || got.Env["OPENAI_BASE_URL"] != "https://proxy.example" {
		t.Errorf("selectors were translated or dropped: env=%v legacy=%v", got.Env, legacy)
	}
}

func TestTranslateLegacyCredentials_UntranslatableStops(t *testing.T) {
	cases := map[string]struct {
		ctx  Context
		want string
	}{
		"literal key": {
			ctx:  Context{Agent: "claude", Secret: "work", Env: map[string]string{"OPENAI_API_KEY": "sk-literal-value"}},
			want: "openai:",
		},
		"other template": {
			ctx:  Context{Agent: "claude", Secret: "work", Env: map[string]string{"OPENAI_API_KEY": "Bearer {{ .secrets.k }}"}},
			want: "OPENAI_API_KEY",
		},
		"no store": {
			ctx:  Context{Agent: "claude", Env: map[string]string{"OPENAI_API_KEY": "{{ .secrets.k }}"}},
			want: "OPENAI_API_KEY",
		},
		"duplicates binding": {
			ctx: Context{Agent: "claude", Secret: "work", Env: map[string]string{"OPENAI_API_KEY": "{{ .secrets.k }}"},
				Credentials: map[string]CredentialBinding{"openai": {Backend: "age", Store: "work", Key: "k"}}},
			want: "openai",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, _, err := TranslateLegacyCredentials(tc.ctx, tc.ctx.Secret)
			if !errors.Is(err, ErrLegacyCredentialEnv) {
				t.Fatalf("error = %v, want ErrLegacyCredentialEnv", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q lacks %q", err, tc.want)
			}
			if strings.Contains(err.Error(), "sk-literal-value") || strings.Contains(err.Error(), "Bearer") {
				t.Errorf("error printed the env value: %s", err)
			}
		})
	}
}

const routeProfiles = `profiles:
  work:
    agent: claude
    credentials:
      anthropic:
        backend: age
        store: work
        key: anthropic_api_key
        for:
          claude: {}
      openrouter:
        backend: age
        store: work
        key: openrouter_api_key
        for:
          claude:
            env: [ANTHROPIC_AUTH_TOKEN]
            set: {ANTHROPIC_BASE_URL: "https://openrouter.ai/api", ANTHROPIC_API_KEY: ""}
          pi:
            env: [OPENROUTER_API_KEY]
      bedrock:
        capabilities: [aws]
        for:
          claude: {set: {CLAUDE_CODE_USE_BEDROCK: "1"}}
          pi: {set: {AWS_REGION: us-east-1}}
    default_credentials: [openrouter]
default_profile: work
`

func TestLoad_CredentialRoutes(t *testing.T) {
	cfg, err := loadYAML(t, routeProfiles)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	creds := cfg.Contexts["work"].Credentials
	or := creds["openrouter"]
	if r, ok := or.Route("openrouter", "claude"); !ok || !slices.Equal(r.Env, []string{"ANTHROPIC_AUTH_TOKEN"}) ||
		r.Set["ANTHROPIC_BASE_URL"] != "https://openrouter.ai/api" {
		t.Errorf("openrouter claude route = %+v, %v", r, ok)
	}
	if r, ok := or.Route("openrouter", "/usr/bin/pi"); !ok || !slices.Equal(r.Env, []string{"OPENROUTER_API_KEY"}) || len(r.Set) != 0 {
		t.Errorf("openrouter pi route = %+v, %v", r, ok)
	}
	if or.AllowsHarness("codex") {
		t.Error("a binding with for: should serve only its for: harnesses")
	}
	if got := or.EnvNames("openrouter"); !slices.Equal(got, []string{"ANTHROPIC_AUTH_TOKEN", "OPENROUTER_API_KEY"}) {
		t.Errorf("EnvNames = %v, want every route's key variables", got)
	}
	if r, ok := creds["anthropic"].Route("anthropic", "claude"); !ok || !slices.Equal(r.Env, []string{"ANTHROPIC_API_KEY"}) {
		t.Errorf("a built-in route without env: should use the built-in variable, got %+v", r)
	}
	bedrock := creds["bedrock"]
	if !bedrock.Keyless() || !slices.Equal(bedrock.Capabilities, []string{"aws"}) {
		t.Errorf("bedrock = %+v, want keyless with capabilities [aws]", bedrock)
	}
	if r, _ := bedrock.Route("bedrock", "pi"); len(r.Env) != 0 || r.Set["AWS_REGION"] != "us-east-1" {
		t.Errorf("bedrock pi route = %+v", r)
	}
	old := CredentialBinding{Backend: "age", Store: "w", Key: "k", Env: []string{"CORP"}, Harnesses: []string{"pi"}}
	if r, ok := old.Route("corp", "pi"); !ok || !slices.Equal(r.Env, []string{"CORP"}) {
		t.Errorf("env:/harnesses: shorthand route = %+v, %v", r, ok)
	}
	if _, ok := old.Route("corp", "claude"); ok {
		t.Error("shorthand route should not serve a harness outside harnesses:")
	}
}

func TestLoad_InvalidCredentialRoutesFail(t *testing.T) {
	cases := map[string]struct{ entry, want string }{
		"for with harnesses": {"credentials: {anthropic: {backend: age, store: w, key: k, harnesses: [pi], for: {pi: {}}}}", "for:"},
		"for with env":       {"credentials: {anthropic: {backend: age, store: w, key: k, env: [X_KEY], for: {pi: {}}}}", "for:"},
		"for unknown":        {"credentials: {anthropic: {backend: age, store: w, key: k, for: {nosuch: {}}}}", "nosuch"},
		"custom route env":   {"credentials: {corp: {backend: age, store: w, key: k, for: {pi: {}}}}", "env"},
		"keyless no effect":  {"credentials: {bedrock: {for: {claude: {}}}}", "capabilities"},
		"keyless env":        {"credentials: {bedrock: {for: {claude: {env: [X_KEY], set: {A: b}}}}}", "store"},
		"set reserved":       {"credentials: {anthropic: {backend: age, store: w, key: k, for: {claude: {set: {PATH: /x}}}}}", "PATH"},
		"set bad name":       {"credentials: {anthropic: {backend: age, store: w, key: k, for: {claude: {set: {\"BAD-N\": x}}}}}", "BAD-N"},
		"set secret value":   {"credentials: {bedrock: {capabilities: [aws], for: {claude: {set: {ANTHROPIC_API_KEY: sk}}}}}", "ANTHROPIC_API_KEY"},
		"defaults collide": {"credentials: {anthropic: {backend: age, store: w, key: k}, " +
			"openai: {backend: age, store: w, key: k, for: {claude: {env: [ANTHROPIC_API_KEY]}}}}\n    default_credentials: [anthropic, openai]", "ANTHROPIC_API_KEY"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := loadYAML(t, "profiles:\n  work:\n    agent: claude\n    "+tc.entry+"\n")
			if !errors.Is(err, ErrInvalidCredentials) {
				t.Fatalf("Load() error = %v, want ErrInvalidCredentials", err)
			}
			if !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "work") {
				t.Errorf("error %q should name profile work and %s", err, tc.want)
			}
		})
	}
	// Blanking a key variable is allowed.
	if _, err := loadYAML(t, routeProfiles); err != nil {
		t.Errorf("set: ANTHROPIC_API_KEY: \"\" should load: %v", err)
	}
}
