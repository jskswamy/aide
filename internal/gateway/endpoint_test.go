package gateway

import (
	"context"
	"testing"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/gatewaypreset"
)

func TestEndpoint_LegacyGatewayUnchanged(t *testing.T) {
	got := Endpoint(config.GatewayConfig{BaseURL: "https://gw.example.com/v1/"})
	want := EndpointSpec{BaseURL: "https://gw.example.com", API: gatewaypreset.APIAnthropicMessages, Header: gatewaypreset.HeaderBearer}
	if got != want {
		t.Errorf("Endpoint() = %+v, want %+v", got, want)
	}
	if u := got.ModelsURL(); u != "https://gw.example.com/v1/models" {
		t.Errorf("ModelsURL() = %q, want today's <base>/v1/models", u)
	}
}

func TestEndpointSpec_ModelsURL(t *testing.T) {
	for base, want := range map[string]string{
		"https://api.anthropic.com":    "https://api.anthropic.com/v1/models",
		"https://api.openai.com/v1":    "https://api.openai.com/v1/models",
		"https://api.z.ai/api/paas/v4": "https://api.z.ai/api/paas/v4/models",
		"https://openrouter.ai/api":    "https://openrouter.ai/api/v1/models",
		"https://openrouter.ai/api/v1": "https://openrouter.ai/api/v1/models",
		"http://127.0.0.1:11434/v1/":   "http://127.0.0.1:11434/v1/models",
		"https://gw.example.com/vault": "https://gw.example.com/vault/v1/models",
	} {
		if got := (EndpointSpec{BaseURL: base}).ModelsURL(); got != want {
			t.Errorf("ModelsURL(%q) = %q, want %q", base, got, want)
		}
	}
}

func TestFetchModels_HeadersFollowEndpoint(t *testing.T) {
	for _, tc := range []struct {
		name                string
		api                 gatewaypreset.API
		header              gatewaypreset.Header
		wantAuth, wantXKey  string
		wantAnthropicVerHdr bool
	}{
		{"anthropic x-api-key", gatewaypreset.APIAnthropicMessages, gatewaypreset.HeaderXAPIKey, "", testToken, true},
		{"anthropic bearer", gatewaypreset.APIAnthropicMessages, gatewaypreset.HeaderBearer, "Bearer " + testToken, "", true},
		{"openai bearer", gatewaypreset.APIOpenAIChat, gatewaypreset.HeaderBearer, "Bearer " + testToken, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, req := modelsServer(t, `{"data":[{"id":"m1"}]}`)
			ep := EndpointSpec{BaseURL: srv.URL, API: tc.api, Header: tc.header}
			if _, err := FetchModels(context.Background(), nil, ep, testToken); err != nil {
				t.Fatal(err)
			}
			if got := req.Header.Get("Authorization"); got != tc.wantAuth {
				t.Errorf("Authorization = %q, want %q", got, tc.wantAuth)
			}
			if got := req.Header.Get("x-api-key"); got != tc.wantXKey {
				t.Errorf("x-api-key = %q, want %q", got, tc.wantXKey)
			}
			if got := req.Header.Get("anthropic-version") != ""; got != tc.wantAnthropicVerHdr {
				t.Errorf("anthropic-version sent = %v, want %v", got, tc.wantAnthropicVerHdr)
			}
		})
	}
}

func TestEndpoint_ResolvesPresetAndCustom(t *testing.T) {
	for _, tc := range []struct {
		name string
		gw   config.GatewayConfig
		want EndpointSpec
	}{
		{"preset openai exact url", config.GatewayConfig{Preset: "openai"},
			EndpointSpec{BaseURL: "https://api.openai.com/v1", API: gatewaypreset.APIOpenAIResponses, Header: gatewaypreset.HeaderBearer}},
		{"preset anthropic x-api-key", config.GatewayConfig{Preset: "anthropic", URL: "https://api.anthropic.com/"},
			EndpointSpec{BaseURL: "https://api.anthropic.com", API: gatewaypreset.APIAnthropicMessages, Header: gatewaypreset.HeaderXAPIKey}},
		{"preset with other url is custom", config.GatewayConfig{Preset: "openrouter-anthropic", BaseURL: "https://proxy.example.com"},
			EndpointSpec{BaseURL: "https://proxy.example.com", API: gatewaypreset.APIAnthropicMessages, Header: gatewaypreset.HeaderBearer}},
		{"preset zai keeps path", config.GatewayConfig{Preset: "zai"},
			EndpointSpec{BaseURL: "https://api.z.ai/api/paas/v4", API: gatewaypreset.APIOpenAIChat, Header: gatewaypreset.HeaderBearer}},
		{"preset keyless", config.GatewayConfig{Preset: "ollama"},
			EndpointSpec{BaseURL: "http://127.0.0.1:11434/v1", API: gatewaypreset.APIOpenAIChat, Header: gatewaypreset.HeaderBearer, Keyless: true}},
		{"custom openai-chat exact", config.GatewayConfig{URL: "https://gw.example.com/v1", API: "openai-chat"},
			EndpointSpec{BaseURL: "https://gw.example.com/v1", API: gatewaypreset.APIOpenAIChat, Header: gatewaypreset.HeaderBearer}},
		{"custom anthropic normalized no-auth", config.GatewayConfig{URL: "https://gw.example.com/v1/", API: "anthropic-messages", NoAuth: true},
			EndpointSpec{BaseURL: "https://gw.example.com", API: gatewaypreset.APIAnthropicMessages, Header: gatewaypreset.HeaderBearer, Keyless: true}},
	} {
		if got := Endpoint(tc.gw); got != tc.want {
			t.Errorf("%s: Endpoint() = %+v, want %+v", tc.name, got, tc.want)
		}
	}
	sw := Endpoint(config.GatewayConfig{Preset: "bedrock", Region: "us-east-1"})
	if sw.Switch == nil || sw.BaseURL != "" || sw.Switch.Capability != "aws" {
		t.Errorf("bedrock Endpoint() = %+v, want a switch and no URL", sw)
	}
}

func TestFetchModels_KeylessSendsNoAuthAndSwitchRefused(t *testing.T) {
	srv, req := modelsServer(t, `{"data":[{"id":"m1"}]}`)
	ep := EndpointSpec{BaseURL: srv.URL, API: gatewaypreset.APIOpenAIChat, Header: gatewaypreset.HeaderBearer, Keyless: true}
	if _, err := FetchModels(context.Background(), nil, ep, testToken); err != nil {
		t.Fatal(err)
	}
	if got := req.Header.Get("Authorization"); got != "" {
		t.Errorf("Authorization = %q, want none for keyless", got)
	}
	if _, err := FetchModels(context.Background(), nil, Endpoint(config.GatewayConfig{Preset: "bedrock"}), ""); err == nil {
		t.Error("FetchModels on a provider switch succeeded, want error")
	}
}

func TestSync_ProviderSwitchIsNoOp(t *testing.T) {
	res, err := Sync(context.Background(), SyncOptions{GatewayName: "bedrock", Gateway: config.GatewayConfig{Preset: "bedrock"}, HomeDir: t.TempDir()})
	if err != nil || res == nil || res.Catalogue != nil || len(res.HarnessStatuses) != 0 {
		t.Errorf("Sync() = %+v, %v; want empty no-op", res, err)
	}
}
