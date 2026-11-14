// Package gatewaypreset is the catalog of built-in gateways: each preset
// is one wire protocol at one exact base URL. It is a leaf package.
package gatewaypreset

import (
	"maps"
	"slices"
)

// Kind is how a gateway authenticates and is delivered.
type Kind string

// Gateway kinds.
const (
	// KindStaticKey sends a stored key to the gateway.
	KindStaticKey Kind = "static-key"
	// KindKeylessLoopback is a local server that needs no key.
	KindKeylessLoopback Kind = "keyless-loopback"
	// KindProviderSwitch turns on a harness's own cloud provider mode.
	KindProviderSwitch Kind = "provider-switch"
)

// API is the wire protocol a gateway speaks.
type API string

// Wire protocols.
const (
	APIAnthropicMessages API = "anthropic-messages"
	APIOpenAIChat        API = "openai-chat"
	APIOpenAIResponses   API = "openai-responses"
)

// Header is how a static key is sent.
type Header string

// Auth headers.
const (
	HeaderXAPIKey Header = "x-api-key"
	HeaderBearer  Header = "bearer"
)

// SwitchSpec is what a provider-switch preset sets on the launch, after
// aide scrubs the inherited environment.
type SwitchSpec struct {
	// Env is set as given (for example CLAUDE_CODE_USE_BEDROCK=1).
	Env map[string]string
	// RegionEnv and ProjectEnv receive the gateway's region and project.
	RegionEnv, ProjectEnv         string
	RequireRegion, RequireProject bool
	// Capability is the cloud capability the switch opens.
	Capability string
	// PermitEnv are variables the switch lets through the scrub.
	PermitEnv []string
}

// Preset is one built-in gateway.
type Preset struct {
	Name      string
	Kind      Kind
	BaseURL   string // empty for provider switches
	API       API    // empty for provider switches
	Header    Header // empty for provider switches
	Harnesses []string
	// ShellVars are the shell variables that commonly hold this preset's
	// key; envscrub scrubs every one.
	ShellVars []string
	Switch    *SwitchSpec
}

var presets = []Preset{
	{Name: "anthropic", Kind: KindStaticKey, BaseURL: "https://api.anthropic.com", API: APIAnthropicMessages, Header: HeaderXAPIKey,
		Harnesses: []string{"claude", "pi", "omp", "opencode"}, ShellVars: []string{"ANTHROPIC_API_KEY"}},
	{Name: "openai", Kind: KindStaticKey, BaseURL: "https://api.openai.com/v1", API: APIOpenAIResponses, Header: HeaderBearer,
		Harnesses: []string{"pi", "omp", "opencode", "codex"}, ShellVars: []string{"OPENAI_API_KEY"}},
	{Name: "codex", Kind: KindStaticKey, BaseURL: "https://api.openai.com/v1", API: APIOpenAIResponses, Header: HeaderBearer,
		Harnesses: []string{"codex"}, ShellVars: []string{"CODEX_API_KEY"}},
	// Codex 0.167 refuses wire_api = "chat", so openai-chat presets never list codex.
	{Name: "openrouter", Kind: KindStaticKey, BaseURL: "https://openrouter.ai/api/v1", API: APIOpenAIChat, Header: HeaderBearer,
		Harnesses: []string{"pi", "omp", "opencode"}, ShellVars: []string{"OPENROUTER_API_KEY"}},
	{Name: "openrouter-anthropic", Kind: KindStaticKey, BaseURL: "https://openrouter.ai/api", API: APIAnthropicMessages, Header: HeaderBearer,
		Harnesses: []string{"claude", "pi", "omp", "opencode"}, ShellVars: []string{"OPENROUTER_API_KEY"}},
	{Name: "opencode-go", Kind: KindStaticKey, BaseURL: "https://opencode.ai/zen/v1", API: APIOpenAIChat, Header: HeaderBearer,
		Harnesses: []string{"pi", "omp", "opencode"}, ShellVars: []string{"OPENCODE_API_KEY"}},
	{Name: "zai", Kind: KindStaticKey, BaseURL: "https://api.z.ai/api/paas/v4", API: APIOpenAIChat, Header: HeaderBearer,
		Harnesses: []string{"pi", "omp", "opencode"}, ShellVars: []string{"ZAI_API_KEY"}},
	{Name: "ollama", Kind: KindKeylessLoopback, BaseURL: "http://127.0.0.1:11434/v1", API: APIOpenAIChat, Header: HeaderBearer,
		Harnesses: []string{"pi", "omp", "opencode"}},
	{Name: "bedrock", Kind: KindProviderSwitch, Harnesses: []string{"claude"}, Switch: &SwitchSpec{
		Env: map[string]string{"CLAUDE_CODE_USE_BEDROCK": "1"}, RegionEnv: "AWS_REGION", RequireRegion: true,
		Capability: "aws", PermitEnv: []string{"AWS_WEB_IDENTITY_TOKEN_FILE"},
	}},
	{Name: "vertex", Kind: KindProviderSwitch, Harnesses: []string{"claude"}, Switch: &SwitchSpec{
		Env: map[string]string{"CLAUDE_CODE_USE_VERTEX": "1"}, RegionEnv: "CLOUD_ML_REGION", ProjectEnv: "ANTHROPIC_VERTEX_PROJECT_ID",
		RequireRegion: true, RequireProject: true, Capability: "gcp",
		PermitEnv: []string{"CLOUD_ML_REGION", "GOOGLE_CLOUD_PROJECT", "GOOGLE_CLOUD_LOCATION", "ANTHROPIC_VERTEX_PROJECT_ID"},
	}},
}

// All returns every preset in catalog order. Callers may modify the
// result.
func All() []Preset {
	out := make([]Preset, len(presets))
	for i, p := range presets {
		out[i] = clonePreset(p)
	}
	return out
}

// Lookup returns the preset named name.
func Lookup(name string) (Preset, bool) {
	for _, p := range presets {
		if p.Name == name {
			return clonePreset(p), true
		}
	}
	return Preset{}, false
}

func clonePreset(p Preset) Preset {
	p.Harnesses = slices.Clone(p.Harnesses)
	p.ShellVars = slices.Clone(p.ShellVars)
	if p.Switch != nil {
		s := *p.Switch
		s.Env = maps.Clone(s.Env)
		s.PermitEnv = slices.Clone(s.PermitEnv)
		p.Switch = &s
	}
	return p
}
