package gatewaypreset_test

import (
	"slices"
	"testing"

	"github.com/jskswamy/aide/internal/capability"
	"github.com/jskswamy/aide/internal/envscrub"
	"github.com/jskswamy/aide/internal/gatewaypreset"
	"github.com/jskswamy/aide/internal/harness"
)

// The spec's preset table (docs/designs/gateway-redesign.md, Built-in
// Presets) is the contract; this pins every column.
func TestPresets_MatchSpecTable(t *testing.T) {
	want := []gatewaypreset.Preset{
		{Name: "anthropic", Kind: gatewaypreset.KindStaticKey, BaseURL: "https://api.anthropic.com", API: gatewaypreset.APIAnthropicMessages, Header: gatewaypreset.HeaderXAPIKey, Harnesses: []string{"claude", "pi", "omp", "opencode"}, ShellVars: []string{"ANTHROPIC_API_KEY"}},
		{Name: "openai", Kind: gatewaypreset.KindStaticKey, BaseURL: "https://api.openai.com/v1", API: gatewaypreset.APIOpenAIResponses, Header: gatewaypreset.HeaderBearer, Harnesses: []string{"pi", "omp", "opencode", "codex"}, ShellVars: []string{"OPENAI_API_KEY"}},
		{Name: "codex", Kind: gatewaypreset.KindStaticKey, BaseURL: "https://api.openai.com/v1", API: gatewaypreset.APIOpenAIResponses, Header: gatewaypreset.HeaderBearer, Harnesses: []string{"codex"}, ShellVars: []string{"CODEX_API_KEY"}},
		{Name: "openrouter", Kind: gatewaypreset.KindStaticKey, BaseURL: "https://openrouter.ai/api/v1", API: gatewaypreset.APIOpenAIChat, Header: gatewaypreset.HeaderBearer, Harnesses: []string{"pi", "omp", "opencode"}, ShellVars: []string{"OPENROUTER_API_KEY"}},
		{Name: "openrouter-anthropic", Kind: gatewaypreset.KindStaticKey, BaseURL: "https://openrouter.ai/api", API: gatewaypreset.APIAnthropicMessages, Header: gatewaypreset.HeaderBearer, Harnesses: []string{"claude", "pi", "omp", "opencode"}, ShellVars: []string{"OPENROUTER_API_KEY"}},
		{Name: "opencode-go", Kind: gatewaypreset.KindStaticKey, BaseURL: "https://opencode.ai/zen/v1", API: gatewaypreset.APIOpenAIChat, Header: gatewaypreset.HeaderBearer, Harnesses: []string{"pi", "omp", "opencode"}, ShellVars: []string{"OPENCODE_API_KEY"}},
		{Name: "zai", Kind: gatewaypreset.KindStaticKey, BaseURL: "https://api.z.ai/api/paas/v4", API: gatewaypreset.APIOpenAIChat, Header: gatewaypreset.HeaderBearer, Harnesses: []string{"pi", "omp", "opencode"}, ShellVars: []string{"ZAI_API_KEY"}},
		{Name: "ollama", Kind: gatewaypreset.KindKeylessLoopback, BaseURL: "http://127.0.0.1:11434/v1", API: gatewaypreset.APIOpenAIChat, Header: gatewaypreset.HeaderBearer, Harnesses: []string{"pi", "omp", "opencode"}},
		{Name: "bedrock", Kind: gatewaypreset.KindProviderSwitch, Harnesses: []string{"claude"}, Switch: &gatewaypreset.SwitchSpec{
			Env: map[string]string{"CLAUDE_CODE_USE_BEDROCK": "1"}, RegionEnv: "AWS_REGION", RequireRegion: true,
			Capability: "aws", PermitEnv: []string{"AWS_WEB_IDENTITY_TOKEN_FILE"},
		}},
		{Name: "vertex", Kind: gatewaypreset.KindProviderSwitch, Harnesses: []string{"claude"}, Switch: &gatewaypreset.SwitchSpec{
			Env: map[string]string{"CLAUDE_CODE_USE_VERTEX": "1"}, RegionEnv: "CLOUD_ML_REGION", ProjectEnv: "ANTHROPIC_VERTEX_PROJECT_ID",
			RequireRegion: true, RequireProject: true, Capability: "gcp",
			PermitEnv: []string{"CLOUD_ML_REGION", "GOOGLE_CLOUD_PROJECT", "GOOGLE_CLOUD_LOCATION", "ANTHROPIC_VERTEX_PROJECT_ID"},
		}},
	}
	got := gatewaypreset.All()
	if len(got) != len(want) {
		t.Fatalf("All() has %d presets, want %d", len(got), len(want))
	}
	for i, w := range want {
		g := got[i]
		if g.Name != w.Name || g.Kind != w.Kind || g.BaseURL != w.BaseURL || g.API != w.API || g.Header != w.Header ||
			!slices.Equal(g.Harnesses, w.Harnesses) || !slices.Equal(g.ShellVars, w.ShellVars) {
			t.Errorf("preset %d = %+v\nwant %+v", i, g, w)
		}
		if (g.Switch == nil) != (w.Switch == nil) {
			t.Errorf("%s: Switch = %+v, want %+v", w.Name, g.Switch, w.Switch)
		} else if w.Switch != nil {
			gs, ws := *g.Switch, *w.Switch
			if gs.RegionEnv != ws.RegionEnv || gs.ProjectEnv != ws.ProjectEnv || gs.RequireRegion != ws.RequireRegion ||
				gs.RequireProject != ws.RequireProject || gs.Capability != ws.Capability ||
				!slices.Equal(gs.PermitEnv, ws.PermitEnv) || len(gs.Env) != len(ws.Env) {
				t.Errorf("%s: Switch = %+v, want %+v", w.Name, gs, ws)
			}
			for k, v := range ws.Env {
				if gs.Env[k] != v {
					t.Errorf("%s: Switch.Env[%s] = %q, want %q", w.Name, k, gs.Env[k], v)
				}
			}
		}
	}
}

func TestPresets_CodexOnlyOnResponses(t *testing.T) {
	// Codex 0.167 refuses wire_api = "chat".
	for _, p := range gatewaypreset.All() {
		if slices.Contains(p.Harnesses, "codex") && p.API != gatewaypreset.APIOpenAIResponses {
			t.Errorf("%s lists codex with API %q", p.Name, p.API)
		}
	}
}

func TestPresets_ShellVarsAreScrubbed(t *testing.T) {
	for _, p := range gatewaypreset.All() {
		for _, v := range p.ShellVars {
			if !envscrub.IsProviderSecretName(v) {
				t.Errorf("%s: shell variable %s is not in envscrub's provider secret names", p.Name, v)
			}
		}
	}
}

func TestPresets_HarnessesAreKnown(t *testing.T) {
	for _, p := range gatewaypreset.All() {
		for _, h := range p.Harnesses {
			if _, ok := harness.Lookup(h); !ok {
				t.Errorf("%s: unknown harness %q", p.Name, h)
			}
		}
	}
}

func TestLookup(t *testing.T) {
	if p, ok := gatewaypreset.Lookup("ollama"); !ok || p.Kind != gatewaypreset.KindKeylessLoopback {
		t.Errorf("Lookup(ollama) = %+v, %v", p, ok)
	}
	if _, ok := gatewaypreset.Lookup("nope"); ok {
		t.Error("Lookup(nope) found a preset")
	}
	p, _ := gatewaypreset.Lookup("anthropic")
	p.Harnesses[0] = "mutated"
	if q, _ := gatewaypreset.Lookup("anthropic"); q.Harnesses[0] != "claude" {
		t.Error("Lookup returned a preset sharing the catalog's slices")
	}
}

// Switch variables aide does not set itself (cloud names, not provider
// selectors) reach the agent only through the capability's env_allow.
func TestPresets_SwitchCloudVarsAllowedByCapability(t *testing.T) {
	caps := capability.Builtins()
	for _, p := range gatewaypreset.All() {
		if p.Switch == nil {
			continue
		}
		c, ok := caps[p.Switch.Capability]
		if !ok {
			t.Errorf("%s: unknown capability %q", p.Name, p.Switch.Capability)
			continue
		}
		for _, v := range p.Switch.PermitEnv {
			if !envscrub.IsProviderName(v) && !slices.Contains(c.EnvAllow, v) {
				t.Errorf("%s: %s is not in capability %s env_allow", p.Name, v, c.Name)
			}
		}
	}
}

func TestByShellVar(t *testing.T) {
	if p, ok := gatewaypreset.ByShellVar("OPENROUTER_API_KEY"); !ok || p.Name != "openrouter" {
		t.Errorf("ByShellVar(OPENROUTER_API_KEY) = %q, %v; want openrouter", p.Name, ok)
	}
	if _, ok := gatewaypreset.ByShellVar("GITHUB_TOKEN"); ok {
		t.Error("ByShellVar(GITHUB_TOKEN) found a preset")
	}
}
