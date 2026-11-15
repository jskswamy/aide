package ui

import (
	"bytes"
	"strings"
	"testing"
)

func TestGatewayLines_Cases(t *testing.T) {
	corp := CredentialInfo{Name: "corp", IsGateway: true, Kind: "gateway", Store: "work", Key: "corp", KeyEnv: "AIDE_GW_CORP_KEY", URL: "https://llm.corp/v1"}
	ollama := CredentialInfo{Name: "ollama", IsGateway: true, Kind: "keyless", URL: "http://127.0.0.1:11434/v1"}
	tests := []struct {
		name  string
		data  BannerData
		label string
		want  []string
		deny  []string
	}{
		{"slot gateway", BannerData{
			ContextName: "work", Harness: "claude",
			GatewayChoices:    []CredentialInfo{{Harness: "claude", Choice: "corp", Source: "repo", Kind: "gateway", Name: "corp", URL: "https://llm.corp/v1", Store: "work", Key: "corp", KeyEnv: "ANTHROPIC_AUTH_TOKEN"}},
			Credentials:       []CredentialInfo{corp},
			AvailableGateways: []GatewayOption{{Name: "openrouter"}},
		}, "Gateway   ", []string{"corp → https://llm.corp/v1   (repo preference)", "key: work/corp → ANTHROPIC_AUTH_TOKEN", "models: not synced yet", "other: openrouter · none (claude login) — aide --gateway <name>"}, nil},
		{"provider switch", BannerData{
			ContextName: "work", Harness: "claude",
			GatewayChoices: []CredentialInfo{{Harness: "claude", Choice: "aws", Source: "profile", Kind: "switch", Name: "aws", URL: "cloud credentials (aws capability)"}},
			Credentials:    []CredentialInfo{{Name: "aws", IsGateway: true, Kind: "switch", URL: "cloud credentials (aws capability)"}},
		}, "Gateway   ", []string{"aws → cloud credentials (aws capability)   (profile work)"}, []string{"key:", "models:", "file:"}},
		{"none", BannerData{
			ContextName: "work", Harness: "claude",
			GatewayChoices:    []CredentialInfo{{Harness: "claude", Choice: "none", Source: "profile"}},
			AvailableGateways: []GatewayOption{{Name: "corp"}, {Name: "openrouter"}},
		}, "Gateway   ", []string{"none — claude login   (profile work)", "available: corp · openrouter — aide --gateway <name>"}, []string{"none (claude login)"}},
		{"multi startup", BannerData{
			ContextName: "work", Harness: "pi",
			GatewayChoices: []CredentialInfo{{Harness: "pi", Choice: "opencode-go", Source: "profile", Kind: "gateway", Name: "opencode-go", Startup: true, Others: 2, URL: "https://opencode.ai/zen/v1", Store: "work", Key: "opencode-go", KeyEnv: "AIDE_GW_OPENCODE_GO_KEY"}},
			Credentials: []CredentialInfo{
				{Name: "opencode-go", IsGateway: true, Kind: "gateway", Startup: true},
				corp, ollama,
			},
		}, "Gateways  ", []string{"opencode-go (startup) → https://opencode.ai/zen/v1   (profile work)", "key: work/opencode-go → AIDE_GW_OPENCODE_GO_KEY", "also in /model: corp (work/corp → AIDE_GW_CORP_KEY)", "ollama (keyless, 127.0.0.1:11434)"}, nil},
		{"multi unset", BannerData{
			ContextName: "work", Harness: "opencode",
			GatewayChoices: []CredentialInfo{{Harness: "opencode", Choice: "unset", Source: "unset", Others: 2, Warnings: []string{"stale"}}},
			Credentials:    []CredentialInfo{corp, ollama},
		}, "Gateways  ", []string{"unset (starts on opencode default)   (unset)", "in /model: corp", "⚠ stale"}, nil},
		{"slot unset", BannerData{
			ContextName: "work", Harness: "claude",
			GatewayChoices: []CredentialInfo{{Harness: "claude", Choice: "unset", Source: "unset"}},
			Credentials:    []CredentialInfo{corp},
		}, "Gateway   ", []string{"unset (would prompt on a terminal, exit 78 in scripts)   (unset)"}, []string{"starts on"}},
		{"file without sync", BannerData{
			ContextName: "work", Harness: "pi",
			GatewayChoices: []CredentialInfo{{Harness: "pi", Choice: "corp", Source: "profile", Kind: "gateway", Name: "corp", Startup: true}},
			Credentials:    []CredentialInfo{{Name: "corp", IsGateway: true, TargetFile: "/x/models.json"}},
		}, "Gateways  ", []string{"file: /x/models.json"}, []string{"()"}},
		{"keyless claude", BannerData{
			ContextName: "work", Harness: "claude",
			GatewayChoices: []CredentialInfo{{Harness: "claude", Choice: "ollama", Source: "flag", Kind: "keyless", Name: "ollama", URL: "http://127.0.0.1:11434"}},
			Credentials:    []CredentialInfo{ollama},
		}, "Gateway   ", []string{"(--gateway)", "keyless (placeholder, login not sent)"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := gatewayLabel(&tt.data); got != tt.label {
				t.Errorf("label = %q, want %q", got, tt.label)
			}
			out := strings.Join(gatewayLines(&tt.data), "\n")
			for _, w := range tt.want {
				if !strings.Contains(out, w) {
					t.Errorf("missing %q in:\n%s", w, out)
				}
			}
			for _, w := range tt.deny {
				if strings.Contains(out, w) {
					t.Errorf("unexpected %q in:\n%s", w, out)
				}
			}
		})
	}
}

func TestGatewayLines_NoCandidatesIsSilent(t *testing.T) {
	d := BannerData{Harness: "codex", GatewayChoices: []CredentialInfo{{Harness: "codex", Choice: "none", Source: "no-candidates"}}}
	if got := gatewayLines(&d); got != nil {
		t.Errorf("lines = %v, want none", got)
	}
}

func TestBanner_KeysLineKeepsPlainCredentials(t *testing.T) {
	d := &BannerData{
		ContextName: "work", AgentName: "claude", Harness: "claude",
		GatewayChoices: []CredentialInfo{{Harness: "claude", Choice: "none", Source: "profile"}},
		Credentials:    []CredentialInfo{{Name: "github", Env: []string{"GITHUB_TOKEN"}}, {Name: "corp", IsGateway: true, Env: []string{"ANTHROPIC_AUTH_TOKEN"}}},
	}
	var buf bytes.Buffer
	if err := RenderBanner(&buf, "boxed", d); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "Keys      github (GITHUB_TOKEN)") || strings.Contains(out, "GITHUB_TOKEN), corp") {
		t.Errorf("Keys line wrong:\n%s", out)
	}
	if strings.Contains(out, "Auth") {
		t.Errorf("Auth line must be gone:\n%s", out)
	}
}

func TestGatewaySummary(t *testing.T) {
	views := []CredentialInfo{
		{Harness: "claude", Choice: "corp", Source: "repo", Kind: "gateway", Store: "work", Key: "corp", KeyEnv: "ANTHROPIC_AUTH_TOKEN"},
		{Harness: "codex", Choice: "none", Source: "no-candidates"},
		{Harness: "pi", Choice: "opencode-go", Source: "profile", Kind: "gateway", Store: "work", Key: "opencode-go", KeyEnv: "AIDE_GW_OPENCODE_GO_KEY", Others: 2},
		{Harness: "opencode", Choice: "unset", Source: "unset", Others: 3},
		{Harness: "codex2", Choice: "unset", Source: "unset"},
	}
	got := strings.Join(GatewaySummary("work", views), "\n")
	want := `Gateways (profile work)
  claude    corp         repo preference     work/corp → ANTHROPIC_AUTH_TOKEN
  codex     —            no gateways marked
  pi        opencode-go  profile work        work/opencode-go → AIDE_GW_OPENCODE_GO_KEY  +2 in /model
  opencode  unset        unset               unset (starts on opencode default) · 3 in /model
  codex2    unset        unset               unset (would prompt on a terminal, exit 78 in scripts)`
	if got != want {
		t.Errorf("summary:\n%s\nwant:\n%s", got, want)
	}
}
