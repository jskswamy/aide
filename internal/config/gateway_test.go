package config

import (
	"errors"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

const gatewayProfiles = `profiles:
  work:
    agent: claude
    config_dir_name: work
    gateways:
      corp:
        base_url: https://alpha.gateway.example.com
        refresh_ttl: 24h
        store: work
        key: corp_token
      gw-beta:
        base_url: https://beta.gateway.example.com
        refresh_ttl: 12h
        store: work
        key: beta_token
        harnesses: [opencode, codex]
default_profile: work
`

func TestGateway_LoadAndRoundTrip(t *testing.T) {
	cfg, err := loadYAML(t, gatewayProfiles)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	ctx := cfg.Contexts["work"]
	corp := ctx.Gateways["corp"]
	if corp.BaseURL != "https://alpha.gateway.example.com" || corp.Store != "work" || corp.Key != "corp_token" || corp.RefreshTTL != "24h" {
		t.Errorf("gateways.corp = %+v", corp)
	}
	if got := ctx.Gateways["gw-beta"].Harnesses; !slices.Equal(got, []string{"opencode", "codex"}) {
		t.Errorf("gateways.gw-beta.harnesses = %v", got)
	}
	if len(ctx.Credentials) != 0 || len(ctx.DefaultCredentials) != 0 {
		t.Errorf("Load must not write synthesized bindings into the profile: credentials=%v defaults=%v",
			ctx.Credentials, ctx.DefaultCredentials)
	}

	out, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if strings.Contains(string(out), "credentials") {
		t.Errorf("marshalled config carries synthesized credentials:\n%s", out)
	}
	var back Config
	if err := yaml.Unmarshal(out, &back); err != nil {
		t.Fatalf("re-reading output: %v\n%s", err, out)
	}
	if got := back.Contexts["work"].Gateways; len(got) != 2 || got["corp"].Key != "corp_token" {
		t.Errorf("round trip lost gateways:\n%s", out)
	}
	if strings.Contains(string(out), "harnesses: []") {
		t.Errorf("omitted harnesses must stay omitted:\n%s", out)
	}
}

func TestGateway_EffectiveHarnessesAndTTL(t *testing.T) {
	gw := GatewayConfig{}
	if got := gw.EffectiveHarnesses(); !slices.Equal(got, []string{"claude", "pi", "opencode", "codex"}) {
		t.Errorf("EffectiveHarnesses() = %v, want all four", got)
	}
	gw.Harnesses = []string{"pi"}
	if got := gw.EffectiveHarnesses(); !slices.Equal(got, []string{"pi"}) {
		t.Errorf("EffectiveHarnesses() = %v, want [pi]", got)
	}
	if ttl, err := (GatewayConfig{}).EffectiveTTL(); err != nil || ttl != 24*time.Hour {
		t.Errorf("EffectiveTTL() = %v, %v; want 24h", ttl, err)
	}
	if ttl, err := (GatewayConfig{RefreshTTL: "90m"}).EffectiveTTL(); err != nil || ttl != 90*time.Minute {
		t.Errorf("EffectiveTTL(90m) = %v, %v", ttl, err)
	}
	if _, err := (GatewayConfig{RefreshTTL: "soon"}).EffectiveTTL(); err == nil {
		t.Error("EffectiveTTL(soon) succeeded, want error")
	}
	if got := (GatewayConfig{Key: "corp_token"}).EnvVarName(); got != "CORP_TOKEN" {
		t.Errorf("EnvVarName() = %q, want CORP_TOKEN", got)
	}
}

func TestGateway_NormalizeBaseURL(t *testing.T) {
	cases := map[string]string{
		"https://gw.example.com":          "https://gw.example.com",
		"https://gw.example.com/":         "https://gw.example.com",
		"https://gw.example.com/v1":       "https://gw.example.com",
		"https://gw.example.com/v1/":      "https://gw.example.com",
		"https://gw.example.com/llm/v1/":  "https://gw.example.com/llm",
		"https://gw.example.com/v1beta":   "https://gw.example.com/v1beta",
		"http://127.0.0.1:18080/v1":       "http://127.0.0.1:18080",
		"  https://gw.example.com/v1//  ": "https://gw.example.com",
	}
	for in, want := range cases {
		if got := NormalizeGatewayBaseURL(in); got != want {
			t.Errorf("NormalizeGatewayBaseURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestGateway_ValidateGateways(t *testing.T) {
	valid := GatewayConfig{BaseURL: "https://gw.example.com", Store: "work", Key: "corp_token"}
	with := func(f func(*GatewayConfig)) GatewayConfig {
		gw := valid
		f(&gw)
		return gw
	}
	cases := map[string]struct {
		name  string
		gw    GatewayConfig
		creds map[string]CredentialBinding
		never []string
		want  string // "" means valid
	}{
		"valid":                 {name: "corp", gw: valid},
		"loopback http":         {name: "corp", gw: with(func(g *GatewayConfig) { g.BaseURL = "http://127.0.0.1:8080" })},
		"localhost http":        {name: "corp", gw: with(func(g *GatewayConfig) { g.BaseURL = "http://localhost:4000/v1" })},
		"ipv6 loopback http":    {name: "corp", gw: with(func(g *GatewayConfig) { g.BaseURL = "http://[::1]:4000" })},
		"remote http":           {name: "corp", gw: with(func(g *GatewayConfig) { g.BaseURL = "http://gw.example.com" }), want: "https"},
		"loopback lookalike":    {name: "corp", gw: with(func(g *GatewayConfig) { g.BaseURL = "http://localhost.evil.com" }), want: "https"},
		"other scheme":          {name: "corp", gw: with(func(g *GatewayConfig) { g.BaseURL = "ftp://gw.example.com" }), want: "https"},
		"no host":               {name: "corp", gw: with(func(g *GatewayConfig) { g.BaseURL = "https://" }), want: "host"},
		"empty url":             {name: "corp", gw: with(func(g *GatewayConfig) { g.BaseURL = "" }), want: "base_url"},
		"userinfo":              {name: "corp", gw: with(func(g *GatewayConfig) { g.BaseURL = "https://u:p@gw.example.com" }), want: "credentials"},
		"query":                 {name: "corp", gw: with(func(g *GatewayConfig) { g.BaseURL = "https://gw.example.com?x=1" }), want: "query"},
		"fragment":              {name: "corp", gw: with(func(g *GatewayConfig) { g.BaseURL = "https://gw.example.com#x" }), want: "fragment"},
		"bad name":              {name: "../evil", gw: valid, want: "name"},
		"bad store":             {name: "corp", gw: with(func(g *GatewayConfig) { g.Store = "../x" }), want: "store"},
		"empty store":           {name: "corp", gw: with(func(g *GatewayConfig) { g.Store = "" }), want: "store"},
		"bad ttl":               {name: "corp", gw: with(func(g *GatewayConfig) { g.RefreshTTL = "tomorrow" }), want: "refresh_ttl"},
		"negative ttl":          {name: "corp", gw: with(func(g *GatewayConfig) { g.RefreshTTL = "-1h" }), want: "refresh_ttl"},
		"bad key":               {name: "corp", gw: with(func(g *GatewayConfig) { g.Key = "corp-token" }), want: "key"},
		"empty key":             {name: "corp", gw: with(func(g *GatewayConfig) { g.Key = "" }), want: "key"},
		"loader key":            {name: "corp", gw: with(func(g *GatewayConfig) { g.Key = "ld_preload" }), want: "LD_PRELOAD"},
		"dyld key":              {name: "corp", gw: with(func(g *GatewayConfig) { g.Key = "dyld_insert_libraries" }), want: "DYLD_INSERT_LIBRARIES"},
		"proxy key":             {name: "corp", gw: with(func(g *GatewayConfig) { g.Key = "https_proxy" }), want: "HTTPS_PROXY"},
		"node options key":      {name: "corp", gw: with(func(g *GatewayConfig) { g.Key = "node_options" }), want: "NODE_OPTIONS"},
		"path key":              {name: "corp", gw: with(func(g *GatewayConfig) { g.Key = "path" }), want: "PATH"},
		"aide key":              {name: "corp", gw: with(func(g *GatewayConfig) { g.Key = "aide_token" }), want: "AIDE_TOKEN"},
		"config dir key":        {name: "corp", gw: with(func(g *GatewayConfig) { g.Key = "claude_config_dir" }), want: "CLAUDE_CONFIG_DIR"},
		"never allow env":       {name: "corp", gw: valid, never: []string{"CORP_TOKEN"}, want: "never_allow_env"},
		"unknown harness":       {name: "corp", gw: with(func(g *GatewayConfig) { g.Harnesses = []string{"gemini"} }), want: "harnesses"},
		"duplicate harness":     {name: "corp", gw: with(func(g *GatewayConfig) { g.Harnesses = []string{"pi", "pi"} }), want: "harnesses"},
		"collides with binding": {name: "corp", gw: valid, creds: map[string]CredentialBinding{"corp": {}}, want: "credentials"},
	}
	for label, tc := range cases {
		t.Run(label, func(t *testing.T) {
			ctx := Context{Gateways: map[string]GatewayConfig{tc.name: tc.gw}, Credentials: tc.creds}
			err := ValidateGateways("profiles.work", ctx, tc.never)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("ValidateGateways() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidateGateways() = nil, want error mentioning %q", tc.want)
			}
			if !errors.Is(err, ErrInvalidGateway) {
				t.Errorf("err = %v, want ErrInvalidGateway", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestGateway_InvalidGatewayFailsLoad(t *testing.T) {
	body := strings.Replace(gatewayProfiles, "https://alpha.gateway.example.com", "http://alpha.gateway.example.com", 1)
	if _, err := loadYAML(t, body); !errors.Is(err, ErrInvalidGateway) {
		t.Fatalf("Load() error = %v, want ErrInvalidGateway", err)
	}
}

func TestGateway_SynthesizeCredentials(t *testing.T) {
	ctx := Context{
		Gateways: map[string]GatewayConfig{
			"corp": {BaseURL: "https://alpha.gateway.example.com/v1/", Store: "work", Key: "corp_token"},
		},
	}
	got, err := SynthesizeGatewayCredentials(ctx)
	if err != nil {
		t.Fatalf("SynthesizeGatewayCredentials() error = %v", err)
	}
	if ctx.Credentials != nil || ctx.DefaultCredentials != nil {
		t.Fatalf("input context mutated: %+v", ctx)
	}
	b, ok := got.Credentials["corp"]
	if !ok {
		t.Fatalf("no synthesized binding: %+v", got.Credentials)
	}
	if b.Backend != CredentialBackendAge || b.Store != "work" || b.Key != "corp_token" {
		t.Errorf("binding = %+v", b)
	}
	claude, ok := b.Route("corp", "claude")
	if !ok || !slices.Equal(claude.Env, []string{"ANTHROPIC_AUTH_TOKEN"}) {
		t.Errorf("claude route = %+v, %v", claude, ok)
	}
	wantSet := map[string]string{
		"ANTHROPIC_BASE_URL":                         "https://alpha.gateway.example.com",
		"CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY": "1",
		"ANTHROPIC_API_KEY":                          "",
	}
	for k, v := range wantSet {
		if gotV, ok := claude.Set[k]; !ok || gotV != v {
			t.Errorf("claude set %s = %q (present %v), want %q", k, gotV, ok, v)
		}
	}
	for _, h := range []string{"pi", "opencode", "codex"} {
		r, ok := b.Route("corp", h)
		if !ok || !slices.Equal(r.Env, []string{"CORP_TOKEN"}) || len(r.Set) != 0 {
			t.Errorf("%s route = %+v, %v; want env [CORP_TOKEN]", h, r, ok)
		}
	}
	if _, ok := b.Route("corp", "gemini"); ok {
		t.Error("gateway binding must not serve gemini")
	}
	if !slices.Equal(got.DefaultCredentials, []string{"corp"}) {
		t.Errorf("DefaultCredentials = %v, want [corp]", got.DefaultCredentials)
	}
	if err := validateBinding("corp", b); err != nil {
		t.Errorf("synthesized binding fails validation: %v", err)
	}
}

func TestGateway_SynthesizeRestrictedHarnesses(t *testing.T) {
	ctx := Context{Gateways: map[string]GatewayConfig{
		"beta": {BaseURL: "https://beta.example.com", Store: "work", Key: "beta_token", Harnesses: []string{"opencode", "codex"}},
	}}
	got, err := SynthesizeGatewayCredentials(ctx)
	if err != nil {
		t.Fatal(err)
	}
	b := got.Credentials["beta"]
	if got := slices.Sorted(maps.Keys(b.For)); !slices.Equal(got, []string{"codex", "opencode"}) {
		t.Errorf("routes = %v, want codex and opencode", got)
	}
}

func TestGateway_SynthesizeDefaultsDoNotCollide(t *testing.T) {
	ctx := Context{
		Credentials: map[string]CredentialBinding{
			"anthropic": {Backend: CredentialBackendAge, Store: "work", Key: "anthropic_api_key"},
		},
		Gateways: map[string]GatewayConfig{
			"alpha": {BaseURL: "https://alpha.example.com", Store: "work", Key: "alpha_token"},
			"beta":  {BaseURL: "https://beta.example.com", Store: "work", Key: "beta_token"},
			"gamma": {BaseURL: "https://gamma.example.com", Store: "work", Key: "gamma_token", Harnesses: []string{"pi"}},
		},
	}

	// No explicit defaults: alpha (first sorted) claims all four
	// gateway harnesses (claude, pi, opencode, codex), so neither beta
	// nor gamma is appended.
	got, err := SynthesizeGatewayCredentials(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.DefaultCredentials, []string{"alpha"}) {
		t.Errorf("DefaultCredentials = %v, want [alpha]", got.DefaultCredentials)
	}
	if err := checkDefaultRoutes(got); err != nil {
		t.Errorf("checkDefaultRoutes() = %v", err)
	}

	// An explicit default (anthropic) claims claude, pi and opencode;
	// alpha is appended for codex, which anthropic does not serve.
	ctx.DefaultCredentials = []string{"anthropic"}
	got, err = SynthesizeGatewayCredentials(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.DefaultCredentials, []string{"anthropic", "alpha"}) {
		t.Errorf("DefaultCredentials = %v, want [anthropic alpha]", got.DefaultCredentials)
	}
	for _, h := range []string{"claude", "pi", "opencode"} {
		if defs := DefaultCredentialsForHarness(got, h); !slices.Equal(defs, []string{"anthropic"}) {
			t.Errorf("DefaultCredentialsForHarness(%s) = %v, want [anthropic]", h, defs)
		}
	}
	if defs := DefaultCredentialsForHarness(got, "codex"); !slices.Equal(defs, []string{"alpha"}) {
		t.Errorf("DefaultCredentialsForHarness(codex) = %v, want [alpha]", defs)
	}
	if !slices.Equal(ctx.DefaultCredentials, []string{"anthropic"}) {
		t.Errorf("input DefaultCredentials mutated: %v", ctx.DefaultCredentials)
	}

	// A gateway named explicitly in default_credentials claims all four
	// harnesses and is not added twice.
	ctx.DefaultCredentials = []string{"beta"}
	got, err = SynthesizeGatewayCredentials(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.DefaultCredentials, []string{"beta"}) {
		t.Errorf("DefaultCredentials = %v, want [beta]", got.DefaultCredentials)
	}
}

func TestGateway_ExplicitDefaultNamesGatewayLoads(t *testing.T) {
	body := strings.Replace(gatewayProfiles, "default_profile: work", "    default_credentials: [gw-beta]\ndefault_profile: work", 1)
	cfg, err := loadYAML(t, body)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := cfg.Contexts["work"].DefaultCredentials; !slices.Equal(got, []string{"gw-beta"}) {
		t.Errorf("default_credentials = %v, want [gw-beta]", got)
	}
}

func TestGateway_ProjectOverrideRejectsGateways(t *testing.T) {
	var po ProjectOverride
	err := yaml.Unmarshal([]byte("gateways:\n  evil:\n    base_url: https://evil.example.com\n"), &po)
	if !errors.Is(err, ErrProfileNotProjectScoped) {
		t.Fatalf("err = %v, want ErrProfileNotProjectScoped", err)
	}
}

func TestGateway_CredentialScrubNames(t *testing.T) {
	cfg, err := loadYAML(t, gatewayProfiles)
	if err != nil {
		t.Fatal(err)
	}
	got := cfg.CredentialScrubNames()
	for _, want := range []string{
		"CORP_TOKEN", "BETA_TOKEN", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_BASE_URL",
		"ANTHROPIC_API_KEY", "CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY",
	} {
		if !slices.Contains(got, want) {
			t.Errorf("CredentialScrubNames() = %v, missing %s", got, want)
		}
	}
}

func TestGateway_ValidateRejectsLegacyCredentialNameClash(t *testing.T) {
	ctx := Context{
		Secret:   "work",
		Env:      map[string]string{"OPENAI_API_KEY": "{{ .secrets.openai_key }}"},
		Gateways: map[string]GatewayConfig{"openai": {BaseURL: "https://openai.corp.example", Store: "work", Key: "openai_gw_token"}},
	}
	err := ValidateGateways("profiles.work", ctx, nil)
	if !errors.Is(err, ErrInvalidGateway) || !strings.Contains(err.Error(), "OPENAI_API_KEY") {
		t.Fatalf("ValidateGateways() = %v, want a clash with the env: credential", err)
	}
}

func TestGateway_ValidateRejectsProviderVariableKeys(t *testing.T) {
	for _, key := range []string{"anthropic_api_key", "openai_api_key", "anthropic_base_url", "anthropic_auth_token"} {
		ctx := Context{Gateways: map[string]GatewayConfig{"corp": {BaseURL: "https://gw.example.com", Store: "work", Key: key}}}
		err := ValidateGateways("profiles.work", ctx, nil)
		if err == nil || !strings.Contains(err.Error(), strings.ToUpper(key)) {
			t.Errorf("key %s: ValidateGateways() = %v, want rejection", key, err)
		}
	}
}
