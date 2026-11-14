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
