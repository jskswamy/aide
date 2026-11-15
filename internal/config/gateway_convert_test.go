package config

import (
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestConvertLegacyGateways(t *testing.T) {
	gw := func(h ...string) GatewayConfig {
		return GatewayConfig{BaseURL: "https://gw.example.com/", Store: "s", Key: "k", RefreshTTL: "1h", Harnesses: h}
	}
	tests := []struct {
		name        string
		ctx         Context
		wantNames   []string
		wantRenames map[string]map[string]string
		check       func(t *testing.T, got map[string]GatewayConfig)
	}{
		{
			name:      "codex not used",
			ctx:       Context{Gateways: map[string]GatewayConfig{"a": gw("claude", "pi")}},
			wantNames: []string{"a"},
			check: func(t *testing.T, got map[string]GatewayConfig) {
				if got["a"].API != apiAnthropicMessages || !reflect.DeepEqual(got["a"].Harnesses, []string{"claude", "pi"}) {
					t.Errorf("a = %+v", got["a"])
				}
			},
		},
		{
			name:        "codex only converts in place",
			ctx:         Context{Gateways: map[string]GatewayConfig{"a": gw("codex")}},
			wantNames:   []string{"a"},
			wantRenames: nil,
			check: func(t *testing.T, got map[string]GatewayConfig) {
				if got["a"].API != apiOpenAIResponses || got["a"].URL != "https://gw.example.com/v1" {
					t.Errorf("a = %+v", got["a"])
				}
			},
		},
		{
			name:        "default credentials select codex: split",
			ctx:         Context{Gateways: map[string]GatewayConfig{"a": {BaseURL: "https://gw.example.com", Store: "s", Key: "k", RefreshTTL: "2h"}}},
			wantNames:   []string{"a", "a-codex"},
			wantRenames: map[string]map[string]string{"codex": {"a": "a-codex"}},
			check: func(t *testing.T, got map[string]GatewayConfig) {
				c := got["a-codex"]
				if c.API != apiOpenAIResponses || c.URL != "https://gw.example.com/v1" ||
					c.Store != "s" || c.Key != "k" || c.RefreshTTL != "2h" || !reflect.DeepEqual(c.Harnesses, []string{"codex"}) || c.Model != "" {
					t.Errorf("a-codex = %+v", c)
				}
				for _, h := range got["a"].Harnesses {
					if h == "codex" {
						t.Errorf("a still serves codex: %+v", got["a"])
					}
				}
			},
		},
		{
			name: "sibling name avoids collisions",
			ctx: Context{Gateways: map[string]GatewayConfig{
				"a":       {BaseURL: "https://gw.example.com", Harnesses: []string{"claude", "codex"}},
				"a-codex": {BaseURL: "https://other.example.com", Harnesses: []string{"claude"}},
			}},
			wantNames:   []string{"a", "a-codex", "a-codex-2"},
			wantRenames: map[string]map[string]string{"codex": {"a": "a-codex-2"}},
		},
		{
			name:        "reserved name renamed",
			ctx:         Context{Gateways: map[string]GatewayConfig{"none": gw("claude")}},
			wantNames:   []string{"none-gw"},
			wantRenames: map[string]map[string]string{"claude": {"none": "none-gw"}},
		},
		{
			name:      "new schema entries pass through",
			ctx:       Context{Gateways: map[string]GatewayConfig{"n": {URL: "https://x.example.com", API: "openai-responses", Harnesses: []string{"codex"}}}},
			wantNames: []string{"n"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, renames := ConvertLegacyGateways(tt.ctx)
			var names []string
			for n := range got {
				names = append(names, n)
			}
			if len(names) != len(tt.wantNames) {
				t.Fatalf("names = %v, want %v", names, tt.wantNames)
			}
			for _, n := range tt.wantNames {
				if _, ok := got[n]; !ok {
					t.Fatalf("missing %q in %v", n, names)
				}
			}
			if !reflect.DeepEqual(renames, tt.wantRenames) {
				t.Errorf("renames = %v, want %v", renames, tt.wantRenames)
			}
			if tt.check != nil {
				tt.check(t, got)
			}
		})
	}
}

func TestGatewayChoiceHelpersAndValidation(t *testing.T) {
	ctx := Context{
		GatewayChoice:     map[string]string{"claude": "none", "codex": "gone", "pi": "a", "omp": ""},
		ConvertedGateways: map[string]GatewayConfig{"a": {URL: "https://x.example.com", Harnesses: []string{"pi"}}},
	}
	if !ctx.ChoiceIsNone("claude") || ctx.ChoiceIsNone("pi") {
		t.Error("ChoiceIsNone")
	}
	if !ctx.ChoiceIsUnset("omp") || !ctx.ChoiceIsUnset("opencode") || ctx.ChoiceIsUnset("pi") {
		t.Error("ChoiceIsUnset")
	}
	got := ValidateGatewayChoices(ctx)
	if len(got) != 1 || !strings.Contains(got[0], "codex") {
		t.Errorf("stale = %v", got)
	}
}

func TestLoadTranslatesLegacyGatewaysAndWritesNewForm(t *testing.T) {
	cfg := &Config{}
	src := "gateways:\n  a:\n    base_url: https://gw.example.com\n    store: s\n    key: k\n"
	var ctx Context
	if err := yaml.Unmarshal([]byte(src), &ctx); err != nil {
		t.Fatal(err)
	}
	cfg.Contexts = map[string]Context{"work": ctx}
	cfg.translateLegacyGateways()
	if !cfg.NeedsGatewayMigration {
		t.Error("a legacy gateway must flag NeedsGatewayMigration")
	}
	if err := WriteConfigTo(cfg, filepath.Join(t.TempDir(), "config.yaml")); !errors.Is(err, ErrNeedsGatewayMigration) {
		t.Errorf("WriteConfigTo() = %v, want ErrNeedsGatewayMigration", err)
	}
	got := cfg.Contexts["work"]
	if _, ok := got.ConvertedGateways["a-codex"]; !ok {
		t.Errorf("converted = %v", got.ConvertedGateways)
	}
	if cfg.LegacyRenames["work"]["codex"]["a"] != "a-codex" {
		t.Errorf("renames = %v", cfg.LegacyRenames)
	}
	out, err := yaml.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "a-codex") || strings.Contains(string(out), "base_url") ||
		!strings.Contains(string(out), "url: https://gw.example.com") || !strings.Contains(string(out), "api: anthropic-messages") {
		t.Errorf("gateway must be written in the new form:\n%s", out)
	}
}

func TestConvertLegacyGatewaysAidePrefixRenamed(t *testing.T) {
	for _, old := range []string{"aide-foo", "AIDE-foo"} {
		got, renames := ConvertLegacyGateways(Context{Gateways: map[string]GatewayConfig{
			old: {BaseURL: "https://gw.example.com", Harnesses: []string{"claude"}}}})
		want := "gw-" + old
		if _, ok := got[want]; !ok || len(got) != 1 || renames["claude"][old] != want {
			t.Errorf("%s: got %v renames %v", old, got, renames)
		}
	}
}

func TestConvertLegacyGatewaysKeylessAndReservedCodex(t *testing.T) {
	got, _ := ConvertLegacyGateways(Context{Gateways: map[string]GatewayConfig{
		"a": {BaseURL: "http://127.0.0.1:11434"}}})
	if !got["a"].NoAuth || !got["a-codex"].NoAuth {
		t.Errorf("keyless: %+v", got)
	}
	got, renames := ConvertLegacyGateways(Context{Gateways: map[string]GatewayConfig{
		"none": {BaseURL: "https://gw.example.com", Key: "k"}}})
	if _, ok := got["none-gw-codex"]; !ok || renames["codex"]["none"] != "none-gw-codex" || renames["claude"]["none"] != "none-gw" {
		t.Errorf("got %v renames %v", got, renames)
	}
}

const choiceProfile = `profiles:
  work:
    agent: claude
    config_dir_name: work
    gateway_choice:
      claude: none
      codex: ghost
    gateways:
      a:
        base_url: https://gw.example.com
        store: s
        key: k
default_profile: work
`

func TestLoadDecodesGatewayChoiceAndWarnsStale(t *testing.T) {
	var buf strings.Builder
	old := loadWarnings
	loadWarnings = &buf
	t.Cleanup(func() { loadWarnings = old })
	cfg, err := loadYAML(t, choiceProfile)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	ctx := cfg.Contexts["work"]
	if ctx.GatewayChoice["claude"] != "none" || len(ctx.ConvertedGateways) == 0 {
		t.Errorf("ctx = %+v", ctx)
	}
	if !strings.Contains(buf.String(), `gateway_choice.codex: gateway "ghost" is not defined`) {
		t.Errorf("warnings = %q", buf.String())
	}
}

func TestLoadAcceptsNewFormGatewayWithoutMigration(t *testing.T) {
	body := strings.Replace(choiceProfile, "base_url:", "url:", 1)
	cfg, err := loadYAML(t, body)
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}
	if cfg.NeedsGatewayMigration {
		t.Error("a gateway in the new form needs no migration")
	}
}

func TestLegacyGatewayChoice(t *testing.T) {
	legacy := func(h ...string) GatewayConfig {
		return GatewayConfig{BaseURL: "https://gw.example.com", Store: "s", Key: "k", Harnesses: h}
	}
	convert := func(ctx Context) (Context, map[string]map[string]string) {
		ctx.ConvertedGateways, _ = ConvertLegacyGateways(ctx)
		_, renames := ConvertLegacyGateways(ctx)
		return ctx, renames
	}
	t.Run("sorted rule picks the first gateway", func(t *testing.T) {
		ctx, renames := convert(Context{Gateways: map[string]GatewayConfig{"b": legacy("claude"), "a": legacy("claude")}})
		if got := LegacyGatewayChoice(ctx, "claude", renames); got != "a" {
			t.Errorf("choice = %q, want a", got)
		}
	})
	t.Run("codex follows the split gateway", func(t *testing.T) {
		ctx, renames := convert(Context{Gateways: map[string]GatewayConfig{"a": legacy()}})
		if got := LegacyGatewayChoice(ctx, "codex", renames); got != "a-codex" {
			t.Errorf("choice = %q, want a-codex", got)
		}
	})
	t.Run("no marked gateway leaves it unset", func(t *testing.T) {
		ctx, renames := convert(Context{Gateways: map[string]GatewayConfig{"a": legacy("claude")}})
		if got := LegacyGatewayChoice(ctx, "pi", renames); got != "" {
			t.Errorf("choice = %q, want unset", got)
		}
	})
	t.Run("a provider credential that claims the slot means none", func(t *testing.T) {
		ctx, renames := convert(Context{
			Gateways:           map[string]GatewayConfig{"a": legacy("claude")},
			Credentials:        map[string]CredentialBinding{"anthropic": {Store: "s", Key: "k"}},
			DefaultCredentials: []string{"anthropic"},
		})
		if got := LegacyGatewayChoice(ctx, "claude", renames); got != "none" {
			t.Errorf("choice = %q, want none", got)
		}
	})
}
