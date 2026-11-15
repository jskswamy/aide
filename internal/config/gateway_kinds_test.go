package config

import (
	"maps"
	"slices"
	"strings"
	"testing"
)

func TestGatewayBinding_ClaudeKeyHeader(t *testing.T) {
	keyed := func(gw GatewayConfig) GatewayConfig {
		gw.Store, gw.Key, gw.Harnesses = "work", "k", []string{"claude"}
		return gw
	}
	cases := map[string]struct {
		gw      GatewayConfig
		env     string
		set     map[string]string
		emptied string // the variable the route blanks so no ambient value is sent
	}{
		"x-api-key preset": {
			gw:  keyed(GatewayConfig{Preset: "anthropic", URL: "https://api.anthropic.com", API: "anthropic-messages"}),
			env: "ANTHROPIC_API_KEY", emptied: "ANTHROPIC_AUTH_TOKEN",
			set: map[string]string{"ANTHROPIC_BASE_URL": "https://api.anthropic.com"},
		},
		"preset without url": {
			gw:  keyed(GatewayConfig{Preset: "anthropic"}),
			env: "ANTHROPIC_API_KEY", emptied: "ANTHROPIC_AUTH_TOKEN",
			set: map[string]string{"ANTHROPIC_BASE_URL": "https://api.anthropic.com"},
		},
		"bearer preset": {
			gw:  keyed(GatewayConfig{Preset: "openrouter-anthropic", BaseURL: "https://openrouter.ai/api"}),
			env: "ANTHROPIC_AUTH_TOKEN", emptied: "ANTHROPIC_API_KEY",
			set: map[string]string{"ANTHROPIC_BASE_URL": "https://openrouter.ai/api"},
		},
		"custom url on the anthropic preset stays bearer": {
			gw:  keyed(GatewayConfig{Preset: "anthropic", BaseURL: "https://proxy.example.com/v1"}),
			env: "ANTHROPIC_AUTH_TOKEN", emptied: "ANTHROPIC_API_KEY",
			set: map[string]string{"ANTHROPIC_BASE_URL": "https://proxy.example.com"},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			r := GatewayBinding("gw", c.gw).For["claude"]
			if !slices.Equal(r.Env, []string{c.env}) {
				t.Errorf("Env = %v, want [%s]", r.Env, c.env)
			}
			if v, ok := r.Set[c.emptied]; !ok || v != "" {
				t.Errorf("Set[%s] = %q, %v; want blanked", c.emptied, v, ok)
			}
			for k, want := range c.set {
				if r.Set[k] != want {
					t.Errorf("Set[%s] = %q, want %q", k, r.Set[k], want)
				}
			}
			if _, ok := r.Set[c.env]; ok {
				t.Errorf("Set must not hold %s, the key's own variable", c.env)
			}
		})
	}
}

func TestGatewayBinding_ProviderSwitch(t *testing.T) {
	b := GatewayBinding("aws", GatewayConfig{Preset: "bedrock", Region: "eu-west-1"})
	r, ok := b.Route("aws", "claude")
	if !ok || len(r.Env) != 0 || !b.Keyless() {
		t.Fatalf("route = %+v, %v; keyless = %v", r, ok, b.Keyless())
	}
	want := map[string]string{"CLAUDE_CODE_USE_BEDROCK": "1", "AWS_REGION": "eu-west-1"}
	if !maps.Equal(r.Set, want) {
		t.Errorf("Set = %v, want %v", r.Set, want)
	}
	if !slices.Equal(b.Capabilities, []string{"aws"}) {
		t.Errorf("Capabilities = %v, want [aws]", b.Capabilities)
	}
	if _, ok := b.Route("aws", "pi"); ok {
		t.Error("a switch serves only claude")
	}
	v := GatewayBinding("v", GatewayConfig{Preset: "vertex", Region: "us-east5", Project: "p1"})
	if got := v.For["claude"].Set["ANTHROPIC_VERTEX_PROJECT_ID"]; got != "p1" || v.For["claude"].Set["CLOUD_ML_REGION"] != "us-east5" ||
		!slices.Equal(v.Capabilities, []string{"gcp"}) {
		t.Errorf("vertex binding = %+v", v)
	}
}

func TestValidateGateways_Kinds(t *testing.T) {
	cases := map[string]struct {
		gw   GatewayConfig
		want string // "" means valid
	}{
		"claude on anthropic-messages":    {gw: GatewayConfig{URL: "https://gw.example.com", API: "anthropic-messages", Harnesses: []string{"claude", "pi"}}},
		"claude on a chat api":            {gw: GatewayConfig{URL: "https://gw.example.com/v1", API: "openai-chat", Harnesses: []string{"claude"}}, want: `"claude"`},
		"codex on chat":                   {gw: GatewayConfig{URL: "https://gw.example.com/v1", API: "openai-chat", Harnesses: []string{"codex"}}, want: `"codex"`},
		"codex on responses":              {gw: GatewayConfig{URL: "https://gw.example.com/v1", API: "openai-responses", Harnesses: []string{"codex", "pi"}}},
		"codex on messages":               {gw: GatewayConfig{URL: "https://gw.example.com", Harnesses: []string{"codex"}}, want: `"codex"`},
		"unknown api":                     {gw: GatewayConfig{URL: "https://gw.example.com", API: "graphql"}, want: "api"},
		"unknown preset":                  {gw: GatewayConfig{Preset: "nope", URL: "https://gw.example.com"}, want: "preset"},
		"preset mark outside its list":    {gw: GatewayConfig{Preset: "openrouter", Harnesses: []string{"claude"}}, want: "openrouter"},
		"preset marks inside its list":    {gw: GatewayConfig{Preset: "openrouter", Harnesses: []string{"pi", "opencode"}, Store: "s", Key: "k"}},
		"anthropic preset for claude":     {gw: GatewayConfig{Preset: "anthropic", Harnesses: []string{"claude"}, Store: "s", Key: "k"}},
		"bedrock":                         {gw: GatewayConfig{Preset: "bedrock", Region: "us-east-1"}},
		"bedrock without region":          {gw: GatewayConfig{Preset: "bedrock"}, want: "region"},
		"vertex":                          {gw: GatewayConfig{Preset: "vertex", Region: "us-east5", Project: "p"}},
		"vertex without region":           {gw: GatewayConfig{Preset: "vertex", Project: "p"}, want: "region"},
		"vertex without project":          {gw: GatewayConfig{Preset: "vertex", Region: "us-east5"}, want: "project"},
		"switch with a key":               {gw: GatewayConfig{Preset: "bedrock", Region: "r", Store: "s", Key: "k"}, want: "switch"},
		"switch with no_auth":             {gw: GatewayConfig{Preset: "bedrock", Region: "r", NoAuth: true}, want: "no_auth"},
		"switch with a url":               {gw: GatewayConfig{Preset: "bedrock", Region: "r", URL: "https://evil.example.com"}, want: "url"},
		"switch marked for pi":            {gw: GatewayConfig{Preset: "bedrock", Region: "r", Harnesses: []string{"pi"}}, want: "bedrock"},
		"legacy gateway keeps every mark": {gw: GatewayConfig{BaseURL: "https://gw.example.com", Harnesses: []string{"claude", "codex"}}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			err := ValidateGateways("profiles.work", Context{Gateways: map[string]GatewayConfig{"gw": c.gw}}, nil)
			switch {
			case c.want == "" && err != nil:
				t.Errorf("error = %v, want valid", err)
			case c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)):
				t.Errorf("error = %v, want it to mention %q", err, c.want)
			}
		})
	}
}

func TestEffectiveHarnesses_FollowThePresetAndWireProtocol(t *testing.T) {
	cases := map[string]struct {
		gw   GatewayConfig
		want []string
	}{
		"openai preset":    {GatewayConfig{Preset: "openai"}, []string{"pi", "omp", "opencode", "codex"}},
		"switch":           {GatewayConfig{Preset: "bedrock", Region: "r"}, []string{"claude"}},
		"custom chat":      {GatewayConfig{URL: "https://gw.example.com/v1", API: "openai-chat"}, []string{"pi", "omp", "opencode"}},
		"custom responses": {GatewayConfig{URL: "https://gw.example.com/v1", API: "openai-responses"}, []string{"pi", "omp", "opencode", "codex"}},
		"custom messages":  {GatewayConfig{URL: "https://gw.example.com"}, []string{"claude", "pi", "omp", "opencode"}},
		"legacy base_url":  {GatewayConfig{BaseURL: "https://gw.example.com"}, DefaultGatewayHarnesses},
	}
	for name, c := range cases {
		if got := c.gw.EffectiveHarnesses(); !slices.Equal(got, c.want) {
			t.Errorf("%s: EffectiveHarnesses() = %v, want %v", name, got, c.want)
		}
	}
}

func TestIsLoopback_UsesThePresetURL(t *testing.T) {
	if !(GatewayConfig{Preset: "ollama"}).IsLoopback() || (GatewayConfig{Preset: "anthropic"}).IsLoopback() {
		t.Error("IsLoopback() must read the preset's url")
	}
}

func TestKeylessSwitchTargetNeedsNoLoopback(t *testing.T) {
	if err := ValidateGatewayTarget("aws", GatewayConfig{Preset: "bedrock", Region: "r"}); err != nil {
		t.Errorf("ValidateGatewayTarget(switch) = %v", err)
	}
}
