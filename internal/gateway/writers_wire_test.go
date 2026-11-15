package gateway

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/config"
)

// Every writer row of the serialization table: the entry each wire
// protocol and key header produces, which Inspect then finds in sync and
// remove takes out again.
func TestWriters_WireProtocols(t *testing.T) {
	const keyVar = "AIDE_GW_CORP_KEY"
	cases := []struct {
		name    string
		gw      config.GatewayConfig
		harness string
		file    string
		want    []string // substrings of the written file
		wantNot []string
	}{
		{"pi openai-chat", config.GatewayConfig{URL: "https://gw.example.com/api/paas/v4", API: "openai-chat", Key: "k"}, "pi", "models.json",
			[]string{`"baseUrl": "https://gw.example.com/api/paas/v4"`, `"api": "openai-completions"`, `"apiKey": "$` + keyVar + `"`, `"authHeader": true`}, []string{`"headers"`}},
		{"pi openai-responses", config.GatewayConfig{URL: "https://gw.example.com/v1", API: "openai-responses", Key: "k"}, "pi", "models.json",
			[]string{`"baseUrl": "https://gw.example.com/v1"`, `"api": "openai-responses"`}, nil},
		{"pi x-api-key", config.GatewayConfig{Preset: "anthropic", Key: "k"}, "pi", "models.json",
			[]string{`"baseUrl": "https://api.anthropic.com"`, `"api": "anthropic-messages"`, `"x-api-key": "$` + keyVar + `"`, `"authHeader": false`}, nil},
		{"pi keyless", config.GatewayConfig{Preset: "ollama"}, "pi", "models.json",
			[]string{`"baseUrl": "http://127.0.0.1:11434/v1"`, `"api": "openai-completions"`, `"apiKey": "aide-no-auth"`, `"authHeader": false`}, nil},
		{"omp openai-chat", config.GatewayConfig{URL: "https://gw.example.com/v1", API: "openai-chat", Key: "k"}, "omp", "models.yml",
			[]string{"baseUrl: https://gw.example.com/v1", "api: openai-completions", "apiKey: " + keyVar, "authHeader: true"}, nil},
		{"omp x-api-key", config.GatewayConfig{Preset: "anthropic", Key: "k"}, "omp", "models.yml",
			[]string{"baseUrl: https://api.anthropic.com", "api: anthropic-messages", "apiKey: " + keyVar}, []string{"authHeader"}},
		{"omp keyless", config.GatewayConfig{Preset: "ollama"}, "omp", "models.yml",
			[]string{"api: openai-completions", "auth: none"}, []string{"apiKey", "authHeader"}},
		{"opencode openai-chat", config.GatewayConfig{URL: "https://gw.example.com/api/paas/v4", API: "openai-chat", Key: "k"}, "opencode", "opencode.jsonc",
			[]string{`"@ai-sdk/openai-compatible"`, `"baseURL":"https://gw.example.com/api/paas/v4"`, `"Authorization":"Bearer {env:` + keyVar + `}"`}, []string{"compatibility", "x-api-key"}},
		{"opencode x-api-key", config.GatewayConfig{Preset: "anthropic", Key: "k"}, "opencode", "opencode.jsonc",
			[]string{`"@ai-sdk/anthropic"`, `"baseURL":"https://api.anthropic.com/v1"`, `"x-api-key":"{env:` + keyVar + `}"`}, []string{"Authorization"}},
		{"opencode keyless", config.GatewayConfig{Preset: "ollama"}, "opencode", "opencode.jsonc",
			[]string{`"@ai-sdk/openai-compatible"`, `"baseURL":"http://127.0.0.1:11434/v1"`}, []string{"apiKey", "env", "Authorization"}},
		{"codex responses", config.GatewayConfig{Preset: "openai", Key: "k"}, "codex", "config.toml",
			[]string{"base_url = 'https://api.openai.com/v1'", "wire_api = 'responses'", "env_key = '" + keyVar + "'"}, nil},
		{"codex keyless", config.GatewayConfig{URL: "http://127.0.0.1:8080/v1", API: "openai-responses", NoAuth: true}, "codex", "config.toml",
			[]string{"base_url = 'http://127.0.0.1:8080/v1'"}, []string{"env_key"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t)
			f.mkdirs(t, c.harness)
			f.cat.BaseURL = Endpoint(c.gw).BaseURL
			path := filepath.Join(f.dir(c.harness), c.file)
			if st, err := MaterializeHarness(f.ctx, "corp", c.gw, f.cat, c.harness, f.home, false); err != nil || st.State != StateSynced {
				t.Fatalf("MaterializeHarness() = %+v, %v", st, err)
			}
			body := strings.ReplaceAll(readFile(t, path), "\": ", "\":") // jsonc and json spacing differ per writer
			for _, w := range c.want {
				if !strings.Contains(body, strings.ReplaceAll(w, "\": ", "\":")) {
					t.Errorf("missing %q in:\n%s", w, body)
				}
			}
			for _, w := range c.wantNot {
				if strings.Contains(body, w) {
					t.Errorf("unexpected %q in:\n%s", w, body)
				}
			}
			if got := InspectHarness(f.ctx, "corp", c.gw, f.cat, c.harness, f.home); got.State != StateSynced {
				t.Errorf("InspectHarness() = %+v, want synced", got)
			}
			if removed, err := RemoveFromHarness(f.ctx, "corp", c.gw, c.harness, f.home); err != nil || !removed {
				t.Errorf("RemoveFromHarness() = %v, %v; want true", removed, err)
			}
		})
	}
}

// Codex loads only the Responses API, and a provider switch has no
// entry anywhere.
func TestWriters_SkipWhatTheyCannotCarry(t *testing.T) {
	f := newFixture(t)
	f.mkdirs(t, "claude", "codex", "pi", "opencode", "omp")
	chat := config.GatewayConfig{URL: "https://gw.example.com/v1", API: "openai-chat", Key: "k"}
	sw := config.GatewayConfig{Preset: "bedrock", Region: "us-east-1"}
	for _, c := range []struct {
		h  string
		gw config.GatewayConfig
	}{{"codex", f.gw}, {"codex", chat}, {"claude", chat}, {"pi", sw}, {"omp", sw}, {"opencode", sw}, {"codex", sw}} {
		if _, err := MaterializeHarness(f.ctx, "corp", c.gw, f.cat, c.h, f.home, false); err != nil {
			t.Fatal(err)
		}
		if got := InspectHarness(f.ctx, "corp", c.gw, f.cat, c.h, f.home); got.State == StateStale {
			t.Errorf("%s InspectHarness() = %+v, want not stale", c.h, got)
		}
		if removed, err := RemoveFromHarness(f.ctx, "corp", c.gw, c.h, f.home); err != nil || removed {
			t.Errorf("%s RemoveFromHarness() = %v, %v; want false", c.h, removed, err)
		}
	}
	for _, h := range []string{"claude", "codex", "pi", "omp", "opencode"} {
		if matches, _ := filepath.Glob(filepath.Join(f.dir(h), "*")); len(matches) != 0 {
			t.Errorf("%s wrote %v", h, matches)
		}
	}
}

// A changed key header or protocol shows as drift so sync rewrites it.
func TestWriters_StaleWhenWireChanges(t *testing.T) {
	f := newFixture(t)
	f.mkdirs(t, "pi", "omp", "opencode")
	f.cat.BaseURL = "https://gw.example.com"
	f.gw.URL = f.gw.BaseURL
	chat := f.gw
	chat.API = "openai-chat"
	for _, h := range []string{"pi", "omp", "opencode"} {
		if _, err := MaterializeHarness(f.ctx, "corp", f.gw, f.cat, h, f.home, false); err != nil {
			t.Fatal(err)
		}
		if got := InspectHarness(f.ctx, "corp", chat, f.cat, h, f.home); got.State != StateStale {
			t.Errorf("%s InspectHarness(after api change) = %+v, want stale", h, got)
		}
		if _, err := MaterializeHarness(f.ctx, "corp", chat, f.cat, h, f.home, false); err != nil {
			t.Fatal(err)
		}
		if got := InspectHarness(f.ctx, "corp", chat, f.cat, h, f.home); got.State != StateSynced {
			t.Errorf("%s InspectHarness(after resync) = %+v, want synced", h, got)
		}
	}
}
