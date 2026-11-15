package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pelletier/go-toml/v2"
	"github.com/tailscale/hujson"

	"github.com/jskswamy/aide/internal/config"
	"gopkg.in/yaml.v3"
)

const testToken = "s3cr3t-gateway-token"

// modelsServer serves body at /v1/models and records the request.
func modelsServer(t *testing.T, body string) (*httptest.Server, *http.Request) {
	t.Helper()
	var got http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = *r.Clone(context.Background())
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

func TestNormalizeBaseURL(t *testing.T) {
	if got := NormalizeBaseURL("https://gw.example.com/v1/"); got != "https://gw.example.com" {
		t.Errorf("NormalizeBaseURL() = %q", got)
	}
}

func TestFetchModels_OpenAIFormat(t *testing.T) {
	srv, req := modelsServer(t, `{"object":"list","data":[
		{"id":"gpt-5","object":"model","owned_by":"openai","context_window":400000,"max_tokens":32768},
		{"id":"claude-sonnet-4-5","display_name":"Claude Sonnet 4.5","owned_by":"anthropic","context_length":200000,"max_output_tokens":16384,"api_key":"upstream-secret"}
	]}`)
	models, err := FetchModels(context.Background(), nil, Endpoint(config.GatewayConfig{BaseURL: srv.URL + "/v1/"}), testToken)
	if err != nil {
		t.Fatalf("FetchModels() error = %v", err)
	}
	want := []Model{
		{ID: "claude-sonnet-4-5", DisplayName: "Claude Sonnet 4.5", Provider: "anthropic", ContextWindow: 200000, MaxOutputTokens: 16384},
		{ID: "gpt-5", DisplayName: "gpt-5", Provider: "openai", ContextWindow: 400000, MaxOutputTokens: 32768},
	}
	if !slices.Equal(models, want) {
		t.Errorf("models = %+v\nwant %+v", models, want)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer "+testToken {
		t.Errorf("Authorization = %q", got)
	}
	if req.Header.Get("anthropic-version") != "2023-06-01" || req.Header.Get("Accept") != "application/json" {
		t.Errorf("headers = %v", req.Header)
	}
	if req.URL.RawQuery != "limit=1000" {
		t.Errorf("query = %q, want limit=1000", req.URL.RawQuery)
	}
}

func TestFetchModels_LiteLLMFormat(t *testing.T) {
	srv, _ := modelsServer(t, `[
		{"slug":"opus","id":"ignored","name":"Opus","provider":"anthropic","model_type":"llm","max_context_tokens":200000},
		{"id":"embed-3","model_type":"embedding"},
		{"id":"off","is_enabled":false},
		{"remote_model":"remote-only","max_tokens":-5},
		{"id":"opus","name":"duplicate"},
		{"name":"no id"}
	]`)
	models, err := FetchModels(context.Background(), nil, Endpoint(config.GatewayConfig{BaseURL: srv.URL}), testToken)
	if err != nil {
		t.Fatal(err)
	}
	want := []Model{
		{ID: "opus", DisplayName: "Opus", Provider: "anthropic", ContextWindow: 200000},
		{ID: "remote-only", DisplayName: "remote-only"},
	}
	if !slices.Equal(models, want) {
		t.Errorf("models = %+v\nwant %+v", models, want)
	}
}

func TestFetchModels_Sanitizes(t *testing.T) {
	long := strings.Repeat("x", 300)
	srv, _ := modelsServer(t, fmt.Sprintf(`{"data":[
		{"id":"evil\u001b]52;c;Zm9v\u0007-model","display_name":"\u001b[31mRed\u009b","owned_by":"%s"},
		{"id":"%s"}
	]}`, long, long))
	models, err := FetchModels(context.Background(), nil, Endpoint(config.GatewayConfig{BaseURL: srv.URL}), testToken)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 {
		t.Fatalf("models = %+v, want the over-long id dropped", models)
	}
	m := models[0]
	for _, s := range []string{m.ID, m.DisplayName, m.Provider} {
		for _, r := range s {
			if r < 0x20 || (r >= 0x7f && r <= 0x9f) {
				t.Errorf("control character %U survived in %q", r, s)
			}
		}
	}
	if m.ID != "evil]52;c;Zm9v-model" || m.DisplayName != "[31mRed" {
		t.Errorf("sanitized = %+v", m)
	}
	if len(m.Provider) != 128 {
		t.Errorf("provider length = %d, want truncated to 128", len(m.Provider))
	}
}

func TestFetchModels_CapsModelCount(t *testing.T) {
	var b strings.Builder
	b.WriteString(`{"data":[`)
	for i := range MaxNormalizedModels + 10 {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `{"id":"m%05d"}`, i)
	}
	b.WriteString(`]}`)
	srv, _ := modelsServer(t, b.String())
	models, err := FetchModels(context.Background(), nil, Endpoint(config.GatewayConfig{BaseURL: srv.URL}), testToken)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != MaxNormalizedModels {
		t.Errorf("len = %d, want %d", len(models), MaxNormalizedModels)
	}
}

func TestFetchModels_RejectsOversizedResponse(t *testing.T) {
	big := `{"data":[{"id":"a","pad":"` + strings.Repeat("x", MaxModelsResponseBytes) + `"}]}`
	srv, _ := modelsServer(t, big)
	_, err := FetchModels(context.Background(), nil, Endpoint(config.GatewayConfig{BaseURL: srv.URL}), testToken)
	if err == nil || !strings.Contains(err.Error(), "MiB") {
		t.Fatalf("err = %v, want a size-limit error", err)
	}
}

func TestFetchModels_HTTPErrorHidesToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusUnauthorized)
	}))
	defer srv.Close()
	_, err := FetchModels(context.Background(), nil, Endpoint(config.GatewayConfig{BaseURL: srv.URL}), testToken)
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("err = %v, want HTTP 401", err)
	}
	if strings.Contains(err.Error(), testToken) {
		t.Errorf("error leaks the token: %v", err)
	}
}

func TestFetchModels_OmitsAuthorizationWhenTokenEmpty(t *testing.T) {
	srv, req := modelsServer(t, `{"data":[{"id":"m1"}]}`)
	if _, err := FetchModels(context.Background(), nil, Endpoint(config.GatewayConfig{BaseURL: srv.URL}), ""); err != nil {
		t.Fatal(err)
	}
	if got := req.Header.Get("Authorization"); got != "" {
		t.Errorf("Authorization = %q, want omitted", got)
	}
}

func TestFetchModels_UnauthorizedIsTyped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusUnauthorized)
	}))
	defer srv.Close()
	_, err := FetchModels(context.Background(), nil, Endpoint(config.GatewayConfig{BaseURL: srv.URL}), testToken)
	if !errors.Is(err, ErrUnauthorized) || !strings.Contains(err.Error(), "401") {
		t.Fatalf("err = %v, want ErrUnauthorized mentioning 401", err)
	}
	if strings.Contains(err.Error(), testToken) {
		t.Errorf("error leaks the token: %v", err)
	}
}

func TestFetchModels_ForbiddenIsTyped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusForbidden)
	}))
	defer srv.Close()
	_, err := FetchModels(context.Background(), nil, Endpoint(config.GatewayConfig{BaseURL: srv.URL}), testToken)
	if !errors.Is(err, ErrForbidden) || !strings.Contains(err.Error(), "403") {
		t.Fatalf("err = %v, want ErrForbidden mentioning 403", err)
	}
}

func TestFetchModels_OtherHTTPErrorUnchanged(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "down", http.StatusBadGateway)
	}))
	defer srv.Close()
	_, err := FetchModels(context.Background(), nil, Endpoint(config.GatewayConfig{BaseURL: srv.URL}), testToken)
	if err == nil || errors.Is(err, ErrUnauthorized) || errors.Is(err, ErrForbidden) || !strings.Contains(err.Error(), "502") {
		t.Fatalf("err = %v, want untyped HTTP 502", err)
	}
}

func TestFetchModels_RejectsCrossOriginRedirect(t *testing.T) {
	var leaked atomic.Bool
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			leaked.Store(true)
		}
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer other.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, strings.Replace(other.URL, "127.0.0.1", "localhost", 1)+"/v1/models", http.StatusFound)
	}))
	defer srv.Close()
	// A caller-supplied client without redirect checks is hardened too.
	_, err := FetchModels(context.Background(), &http.Client{}, Endpoint(config.GatewayConfig{BaseURL: srv.URL}), testToken)
	if err == nil || !strings.Contains(err.Error(), "redirect") {
		t.Fatalf("err = %v, want a redirect error", err)
	}
	if leaked.Load() {
		t.Error("token was sent to the redirect target")
	}
}

func TestCheckRedirect(t *testing.T) {
	mk := func(raw string) *http.Request {
		r, err := http.NewRequest(http.MethodGet, raw, nil)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	via := []*http.Request{mk("https://gw.example.com/v1/models")}
	if err := checkRedirect(mk("https://gw.example.com/api/v1/models"), via); err != nil {
		t.Errorf("same-origin redirect rejected: %v", err)
	}
	if err := checkRedirect(mk("http://gw.example.com/v1/models"), via); err == nil {
		t.Error("https -> http downgrade allowed")
	}
	if err := checkRedirect(mk("https://evil.example.com/v1/models"), via); err == nil {
		t.Error("cross-host redirect allowed")
	}
}

func TestCache_RoundTripAndExpiry(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_STATE_HOME", "")
	fetched := time.Date(2026, 10, 27, 16, 30, 0, 0, time.UTC)
	cat := &CachedCatalogue{Profile: "work", Gateway: "corp", BaseURL: "https://gw.example.com", FetchedAt: fetched,
		Models: []Model{{ID: "m1", DisplayName: "M1"}}}
	if err := WriteCache(home, cat); err != nil {
		t.Fatalf("WriteCache() error = %v", err)
	}
	path, err := CachePath(home, "work", "corp")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, ".local", "state", "aide", "gateways", "work", "corp.json"); path != want {
		t.Errorf("CachePath() = %q, want %q", path, want)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("cache file mode = %v, %v; want 0600", info.Mode().Perm(), err)
	}
	if dinfo, err := os.Stat(filepath.Dir(path)); err != nil || dinfo.Mode().Perm() != 0o700 {
		t.Errorf("cache dir mode = %v, %v; want 0700", dinfo.Mode().Perm(), err)
	}
	got, err := ReadCache(home, "work", "corp")
	if err != nil || got == nil || !got.FetchedAt.Equal(fetched) || len(got.Models) != 1 {
		t.Fatalf("ReadCache() = %+v, %v", got, err)
	}
	if IsExpired(got, 24*time.Hour, fetched.Add(23*time.Hour)) {
		t.Error("fresh cache reported expired")
	}
	if !IsExpired(got, 24*time.Hour, fetched.Add(25*time.Hour)) {
		t.Error("stale cache reported fresh")
	}
	if !IsExpired(nil, 24*time.Hour, fetched) {
		t.Error("missing cache reported fresh")
	}
	if err := RemoveCache(home, "work", "corp"); err != nil {
		t.Fatal(err)
	}
	if got, err := ReadCache(home, "work", "corp"); got != nil || err != nil {
		t.Errorf("ReadCache() after remove = %+v, %v; want nil, nil", got, err)
	}
	if err := RemoveCache(home, "work", "corp"); err != nil {
		t.Errorf("RemoveCache() of a missing cache = %v", err)
	}
}

func TestCache_RejectsPathTraversal(t *testing.T) {
	home := t.TempDir()
	for _, tc := range [][2]string{{"..", "corp"}, {"work", "../x"}, {"a/b", "corp"}, {"work", ""}, {"", "corp"}} {
		if _, err := CachePath(home, tc[0], tc[1]); err == nil {
			t.Errorf("CachePath(%q, %q) succeeded", tc[0], tc[1])
		}
	}
}

// fixture is a profile with its own config dirs under a temp home and
// a fresh catalogue for gateway corp.
type fixture struct {
	home string
	ctx  config.Context
	gw   config.GatewayConfig
	cat  *CachedCatalogue
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	home := t.TempDir()
	t.Setenv("XDG_STATE_HOME", "")
	return fixture{
		home: home,
		ctx:  config.Context{Agent: "claude", Profile: "work", GatewayChoice: map[string]string{"claude": "corp"}},
		gw:   config.GatewayConfig{BaseURL: "https://gw.example.com/v1", Store: "work", Key: "corp_token"},
		cat: &CachedCatalogue{Profile: "work", Gateway: "corp", BaseURL: "https://gw.example.com", FetchedAt: time.Now(),
			Models: []Model{
				{ID: "claude-sonnet-4-5", DisplayName: "Claude Sonnet 4.5", Provider: "anthropic", ContextWindow: 200000, MaxOutputTokens: 16384},
				{ID: "plain", DisplayName: "plain"},
			}},
	}
}

func (f fixture) dir(h string) string { return filepath.Join(f.home, "."+h+"-work") }

func (f fixture) mkdirs(t *testing.T, hs ...string) {
	t.Helper()
	for _, h := range hs {
		if err := os.MkdirAll(f.dir(h), 0o700); err != nil {
			t.Fatal(err)
		}
	}
}

// obj asserts v is a decoded JSON/TOML object.
func obj(t *testing.T, v any) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("%v is not an object", v)
	}
	return m
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), testToken) {
		t.Errorf("%s contains the bearer token", path)
	}
	return string(data)
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestHarnessConfigDir(t *testing.T) {
	home := t.TempDir()
	isolated := config.Context{Profile: "work"}
	if got, _ := HarnessConfigDir(isolated, "pi", home); got != filepath.Join(home, ".pi-work") {
		t.Errorf("isolated pi dir = %q", got)
	}
	shared := config.Context{}
	t.Setenv("CODEX_HOME", "")
	if got, _ := HarnessConfigDir(shared, "codex", home); got != filepath.Join(home, ".codex") {
		t.Errorf("shared codex dir = %q", got)
	}
	if got, _ := HarnessConfigDir(shared, "pi", home); got != filepath.Join(home, ".pi", "agent") {
		t.Errorf("shared pi dir = %q", got)
	}
	t.Setenv("CODEX_HOME", "~/codex-shell")
	if got, _ := HarnessConfigDir(shared, "codex", home); got != filepath.Join(home, ".codex") {
		t.Errorf("shared codex dir ignores shell CODEX_HOME = %q, want %q", got, filepath.Join(home, ".codex"))
	}
	withEnv := config.Context{Env: map[string]string{"CODEX_HOME": "~/codex-env"}}
	if got, _ := HarnessConfigDir(withEnv, "codex", home); got != filepath.Join(home, "codex-env") {
		t.Errorf("codex dir from profile env = %q", got)
	}
	if _, err := HarnessConfigDir(shared, "gemini", home); err == nil {
		t.Error("HarnessConfigDir(gemini) succeeded")
	}
}

func TestMaterialize_PendingWithoutDir(t *testing.T) {
	f := newFixture(t)
	st, err := MaterializeHarness(f.ctx, "corp", f.gw, f.cat, "pi", f.home, false)
	if err != nil || st.State != StatePending {
		t.Fatalf("MaterializeHarness() = %+v, %v; want pending", st, err)
	}
	if _, err := os.Stat(f.dir("pi")); !os.IsNotExist(err) {
		t.Error("pending materialization created the dir")
	}
	st, err = MaterializeHarness(f.ctx, "corp", f.gw, f.cat, "pi", f.home, true)
	if err != nil || st.State != StateSynced {
		t.Fatalf("MaterializeHarness(create) = %+v, %v; want synced", st, err)
	}
	if info, err := os.Stat(f.dir("pi")); err != nil || info.Mode().Perm() != 0o700 {
		t.Errorf("created dir mode = %v, %v; want 0700", info.Mode().Perm(), err)
	}
}

func TestMaterialize_Claude(t *testing.T) {
	f := newFixture(t)
	f.mkdirs(t, "claude")
	st, err := MaterializeHarness(f.ctx, "corp", f.gw, f.cat, "claude", f.home, false)
	if err != nil || st.State != StateSynced {
		t.Fatalf("MaterializeHarness() = %+v, %v", st, err)
	}
	path := filepath.Join(f.dir("claude"), "cache", "gateway-models.json")
	if st.FilePath != path {
		t.Errorf("FilePath = %q, want %q", st.FilePath, path)
	}
	var doc struct {
		BaseURL   string `json:"baseUrl"`
		FetchedAt int64  `json:"fetchedAt"`
		Models    []struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
			Description string `json:"description"`
		} `json:"models"`
	}
	if err := json.Unmarshal([]byte(readFile(t, path)), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.BaseURL != "https://gw.example.com" || doc.FetchedAt != f.cat.FetchedAt.UnixMilli() || len(doc.Models) != 2 {
		t.Errorf("doc = %+v", doc)
	}
	if doc.Models[0].Description != "corp · 200K context" || doc.Models[1].Description != "corp" {
		t.Errorf("descriptions = %q, %q", doc.Models[0].Description, doc.Models[1].Description)
	}
	if got := InspectHarness(f.ctx, "corp", f.gw, f.cat, "claude", f.home); got.State != StateSynced {
		t.Errorf("InspectHarness() = %+v, want synced", got)
	}

	// Another gateway's file is left alone on remove.
	other := f.gw
	other.BaseURL = "https://other.example.com"
	if removed, err := RemoveFromHarness(f.ctx, "other", other, "claude", f.home); err != nil || removed {
		t.Errorf("RemoveFromHarness(other) = %v, %v; want false", removed, err)
	}
	if got := InspectHarness(f.ctx, "other", other, f.cat, "claude", f.home); got.State != StatePending {
		t.Errorf("InspectHarness(other) = %+v, want pending: it is not claude's chosen gateway", got)
	}
	if removed, err := RemoveFromHarness(f.ctx, "corp", f.gw, "claude", f.home); err != nil || !removed {
		t.Errorf("RemoveFromHarness() = %v, %v", removed, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("gateway-models.json survived remove")
	}
}

func TestFormatClaudeDesc(t *testing.T) {
	cases := []struct {
		gw   string
		m    Model
		want string
	}{
		{"corp", Model{Provider: "anthropic", ContextWindow: 200_000}, "corp · 200K context"},
		{"corp", Model{Provider: "custom", ContextWindow: 262_144}, "corp · 262K context"},
		{"corp", Model{Provider: "custom", ContextWindow: 1_048_576}, "corp · 1M context"},
		{"corp", Model{ContextWindow: 1_000_000}, "corp · 1M context"},
		{"corp", Model{ContextWindow: 1_500_000}, "corp · 1.5M context"},
		{"corp", Model{ContextWindow: 512}, "corp · 512 context"},
		{"corp", Model{Provider: "anthropic"}, "corp"},
		{"", Model{ContextWindow: 128_000}, "128K context"},
		{"", Model{}, "Custom gateway model"},
	}
	for _, tc := range cases {
		if got := formatClaudeDesc(tc.gw, tc.m); got != tc.want {
			t.Errorf("formatClaudeDesc(%q, %+v) = %q, want %q", tc.gw, tc.m, got, tc.want)
		}
	}
}

func TestMaterialize_Pi(t *testing.T) {
	f := newFixture(t)
	path := filepath.Join(f.dir("pi"), "models.json")
	writeFile(t, path, `{"providers":{"ollama":{"baseUrl":"http://localhost:11434"}},"theme":"dark"}`)

	if got := InspectHarness(f.ctx, "corp", f.gw, f.cat, "pi", f.home); got.State != StateStale {
		t.Errorf("InspectHarness() before sync = %+v, want stale", got)
	}
	if _, err := MaterializeHarness(f.ctx, "corp", f.gw, f.cat, "pi", f.home, false); err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Theme     string                     `json:"theme"`
		Providers map[string]json.RawMessage `json:"providers"`
	}
	if err := json.Unmarshal([]byte(readFile(t, path)), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Theme != "dark" || doc.Providers["ollama"] == nil {
		t.Errorf("sibling settings lost: %+v", doc)
	}
	var corp map[string]any
	if err := json.Unmarshal(doc.Providers["aide-corp"], &corp); err != nil {
		t.Fatal(err)
	}
	if corp["baseUrl"] != "https://gw.example.com" || corp["apiKey"] != "$AIDE_GW_CORP_KEY" ||
		corp["authHeader"] != true || corp["api"] != "anthropic-messages" {
		t.Errorf("providers.aide-corp = %v", corp)
	}
	models, _ := corp["models"].([]any)
	if len(models) != 2 {
		t.Fatalf("models = %v", corp["models"])
	}
	first, second := obj(t, models[0]), obj(t, models[1])
	if first["id"] != "claude-sonnet-4-5" || first["name"] != "Claude Sonnet 4.5" || first["contextWindow"] != 200000.0 || first["maxTokens"] != 16384.0 {
		t.Errorf("models[0] = %v", first)
	}
	if _, ok := second["contextWindow"]; ok {
		t.Errorf("zero contextWindow written: %v", second)
	}
	if got := InspectHarness(f.ctx, "corp", f.gw, f.cat, "pi", f.home); got.State != StateSynced {
		t.Errorf("InspectHarness() = %+v, want synced", got)
	}

	if removed, err := RemoveFromHarness(f.ctx, "corp", f.gw, "pi", f.home); err != nil || !removed {
		t.Fatalf("RemoveFromHarness() = %v, %v", removed, err)
	}
	body := readFile(t, path)
	if strings.Contains(body, "corp") || !strings.Contains(body, "ollama") || !strings.Contains(body, "dark") {
		t.Errorf("after remove: %s", body)
	}
}

func TestMaterialize_OMP(t *testing.T) {
	f := newFixture(t)
	path := filepath.Join(f.dir("omp"), "models.yml")
	writeFile(t, path, "# keep user comment\nproviders:\n  ollama:\n    baseUrl: http://localhost:11434\n    api: openai-completions\n    auth: none\n")

	if got := InspectHarness(f.ctx, "corp", f.gw, f.cat, "omp", f.home); got.State != StateStale {
		t.Errorf("InspectHarness() before sync = %+v, want stale", got)
	}
	if _, err := MaterializeHarness(f.ctx, "corp", f.gw, f.cat, "omp", f.home, false); err != nil {
		t.Fatal(err)
	}
	raw := readFile(t, path)
	if !strings.Contains(raw, "# keep user comment") {
		t.Errorf("lost user comment:\n%s", raw)
	}
	var doc struct {
		Providers map[string]ompProvider `yaml:"providers"`
	}
	if err := yaml.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Providers["ollama"].BaseURL != "http://localhost:11434" {
		t.Errorf("sibling provider lost: %+v", doc.Providers)
	}
	corp := doc.Providers["aide-corp"]
	if corp.BaseURL != "https://gw.example.com" || corp.APIKey != "AIDE_GW_CORP_KEY" ||
		!corp.AuthHeader || corp.API != "anthropic-messages" || corp.Auth != "" {
		t.Errorf("providers.corp = %+v", corp)
	}
	if len(corp.Models) != 2 || corp.Models[0].ID != "claude-sonnet-4-5" || corp.Models[0].ContextWindow != 200000 {
		t.Errorf("corp.Models = %+v", corp.Models)
	}
	if got := InspectHarness(f.ctx, "corp", f.gw, f.cat, "omp", f.home); got.State != StateSynced {
		t.Errorf("InspectHarness() = %+v, want synced", got)
	}

	// Removing with a different URL leaves the entry untouched.
	otherGW := f.gw
	otherGW.BaseURL = "https://other.example.com"
	if removed, err := RemoveFromHarness(f.ctx, "corp", otherGW, "omp", f.home); err != nil || removed {
		t.Fatalf("RemoveFromHarness(mismatched URL) = %v, %v", removed, err)
	}
	if removed, err := RemoveFromHarness(f.ctx, "corp", f.gw, "omp", f.home); err != nil || !removed {
		t.Fatalf("RemoveFromHarness() = %v, %v", removed, err)
	}
	body := readFile(t, path)
	if strings.Contains(body, "corp") || !strings.Contains(body, "ollama") {
		t.Errorf("after remove:\n%s", body)
	}
}

func TestMaterialize_OMPNoAuthAndExistingModelsYAML(t *testing.T) {
	f := newFixture(t)
	f.gw.Store, f.gw.Key = "", ""
	altPath := filepath.Join(f.dir("omp"), "models.yaml")
	writeFile(t, altPath, "")

	st, err := MaterializeHarness(f.ctx, "corp", f.gw, f.cat, "omp", f.home, false)
	if err != nil || st.State != StateSynced || st.FilePath != altPath || st.EnvVar != "" {
		t.Fatalf("MaterializeHarness() = %+v, %v", st, err)
	}
	var doc struct {
		Providers map[string]ompProvider `yaml:"providers"`
	}
	if err := yaml.Unmarshal([]byte(readFile(t, altPath)), &doc); err != nil {
		t.Fatal(err)
	}
	corp := doc.Providers["aide-corp"]
	if corp.Auth != "none" || corp.APIKey != "" || corp.AuthHeader {
		t.Errorf("no-auth providers.corp = %+v, want auth=none without apiKey/authHeader", corp)
	}
	if got := InspectHarness(f.ctx, "corp", f.gw, f.cat, "omp", f.home); got.State != StateSynced {
		t.Errorf("InspectHarness() = %+v, want synced", got)
	}
}

func TestMaterialize_NoAuthPiOpenCodeCodex(t *testing.T) {
	f := newFixture(t)
	f.gw.Store, f.gw.Key = "", ""
	f.mkdirs(t, "pi", "opencode", "codex")
	writeFile(t, filepath.Join(f.dir("pi"), "models.json"), `{}`)
	writeFile(t, filepath.Join(f.dir("opencode"), "opencode.jsonc"), `{}`)
	writeFile(t, filepath.Join(f.dir("codex"), "config.toml"), ``)

	for _, h := range []string{"pi", "opencode", "codex"} {
		gw := f.gw
		if h == "codex" {
			gw.API = "openai-responses"
		}
		st, err := MaterializeHarness(f.ctx, "corp", gw, f.cat, h, f.home, false)
		if err != nil || st.State != StateSynced || st.EnvVar != "" {
			t.Fatalf("%s MaterializeHarness() = %+v, %v", h, st, err)
		}
		if got := InspectHarness(f.ctx, "corp", gw, f.cat, h, f.home); got.State != StateSynced {
			t.Errorf("%s InspectHarness() = %+v, want synced", h, got)
		}
	}

	piPath := filepath.Join(f.dir("pi"), "models.json")
	var piDoc struct {
		Providers map[string]map[string]any `json:"providers"`
	}
	if err := json.Unmarshal([]byte(readFile(t, piPath)), &piDoc); err != nil {
		t.Fatal(err)
	}
	corp := piDoc.Providers["aide-corp"]
	if corp["apiKey"] != "aide-no-auth" || corp["authHeader"] != false {
		t.Errorf("pi provider = %v, want apiKey=aide-no-auth authHeader=false", corp)
	}
	// Hand-edited literal placeholder keys remain synced; env/command refs or missing keys become stale.
	rawPi := readFile(t, piPath)
	writeFile(t, piPath, strings.Replace(rawPi, `"apiKey": "aide-no-auth"`, `"apiKey": "ollama"`, 1))
	if got := InspectHarness(f.ctx, "corp", f.gw, f.cat, "pi", f.home); got.State != StateSynced {
		t.Errorf("InspectHarness(pi literal placeholder) = %+v, want synced", got)
	}
	for _, bad := range []string{
		strings.Replace(rawPi, `"apiKey": "aide-no-auth"`, `"apiKey": ""`, 1),
		strings.Replace(rawPi, `"apiKey": "aide-no-auth"`, `"apiKey": "$CORP_TOKEN"`, 1),
		strings.Replace(rawPi, `"apiKey": "aide-no-auth"`, `"apiKey": "Bearer $CORP_TOKEN"`, 1),
		strings.Replace(rawPi, `"apiKey": "aide-no-auth"`, `"apiKey": "!echo x"`, 1),
		strings.Replace(rawPi, `"authHeader": false`, `"authHeader": true`, 1),
	} {
		writeFile(t, piPath, bad)
		if got := InspectHarness(f.ctx, "corp", f.gw, f.cat, "pi", f.home); got.State != StateStale {
			t.Errorf("InspectHarness(pi invalid no-auth config) = %+v, want stale", got)
		}
	}

	std, err := hujson.Standardize([]byte(readFile(t, filepath.Join(f.dir("opencode"), "opencode.jsonc"))))
	if err != nil {
		t.Fatal(err)
	}
	var oc map[string]any
	if err := json.Unmarshal(std, &oc); err != nil {
		t.Fatal(err)
	}
	p := obj(t, obj(t, oc["providers"])["aide-corp"])
	settings := obj(t, p["settings"])
	headers, _ := p["headers"].(map[string]any)
	if p["env"] != nil || settings["apiKey"] != nil || (headers != nil && headers["Authorization"] != nil) {
		t.Errorf("opencode provider = %v", p)
	}

	var tomlDoc map[string]any
	if err := toml.Unmarshal([]byte(readFile(t, filepath.Join(f.dir("codex"), "config.toml"))), &tomlDoc); err != nil {
		t.Fatal(err)
	}
	cp := obj(t, obj(t, tomlDoc["model_providers"])["aide-corp"])
	if _, ok := cp["env_key"]; ok {
		t.Errorf("codex env_key present: %v", cp)
	}
}

func TestMaterialize_OpenCode(t *testing.T) {
	f := newFixture(t)
	path := filepath.Join(f.dir("opencode"), "opencode.jsonc")
	writeFile(t, path, `{
  // user comment
  "mcp": {"fs": {"type": "local", "command": ["fs"]}},
  "plugin": ["p1"],
  "provider": {"corp": {"npm": "@ai-sdk/anthropic", "name": "corp", "options": {"baseURL": "https://old.example.com/v1", "apiKey": "{env:CORP_TOKEN}"}}},
  /* trailing */
}`)
	if _, err := MaterializeHarness(f.ctx, "corp", f.gw, f.cat, "opencode", f.home, false); err != nil {
		t.Fatal(err)
	}
	std, err := hujson.Standardize([]byte(readFile(t, path)))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(std, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["mcp"] == nil || doc["plugin"] == nil {
		t.Errorf("sibling keys lost: %v", doc)
	}
	if doc["provider"] != nil {
		t.Errorf("legacy V1 provider section survived migration: %v", doc["provider"])
	}
	if doc["model"] != nil {
		t.Errorf("model = %v; aide must not pin OpenCode's startup model", doc["model"])
	}
	corp := obj(t, obj(t, doc["providers"])["aide-corp"])
	settings := obj(t, corp["settings"])
	headers := obj(t, corp["headers"])
	env, _ := corp["env"].([]any)
	if corp["package"] != "@ai-sdk/anthropic" || corp["name"] != "aide-corp" ||
		len(env) != 1 || env[0] != "AIDE_GW_CORP_KEY" ||
		settings["baseURL"] != "https://gw.example.com/v1" || settings["apiKey"] != "{env:AIDE_GW_CORP_KEY}" ||
		headers["Authorization"] != "Bearer {env:AIDE_GW_CORP_KEY}" {
		t.Errorf("providers.aide-corp = %v", corp)
	}
	models := obj(t, corp["models"])
	sonnet := obj(t, models["claude-sonnet-4-5"])
	limit := obj(t, sonnet["limit"])
	compat := obj(t, sonnet["compatibility"])
	if sonnet["name"] != "Claude Sonnet 4.5" || limit["context"] != 200000.0 || limit["output"] != 16384.0 ||
		compat["supportsThinkingBlockBinding"] != false {
		t.Errorf("models = %v", models)
	}
	if _, ok := obj(t, models["plain"])["limit"]; ok {
		t.Errorf("zero limit written: %v", models["plain"])
	}
	if got := InspectHarness(f.ctx, "corp", f.gw, f.cat, "opencode", f.home); got.State != StateSynced {
		t.Errorf("InspectHarness() = %+v, want synced", got)
	}
	writeFile(t, path, strings.ReplaceAll(readFile(t, path), `"compatibility"`, `"legacy"`))
	if got := InspectHarness(f.ctx, "corp", f.gw, f.cat, "opencode", f.home); got.State != StateStale {
		t.Errorf("InspectHarness() without compatibility = %+v, want stale", got)
	}
	if removed, err := RemoveFromHarness(f.ctx, "corp", f.gw, "opencode", f.home); err != nil || !removed {
		t.Fatalf("RemoveFromHarness() = %v, %v", removed, err)
	}
	if body := readFile(t, path); strings.Contains(body, `"corp"`) || !strings.Contains(body, "fs") {
		t.Errorf("after remove: %s", body)
	}
}

func TestMaterialize_Codex(t *testing.T) {
	f := newFixture(t)
	f.gw.API = "openai-responses"
	path := filepath.Join(f.dir("codex"), "config.toml")
	writeFile(t, path, "model = \"gpt-5\"\n\n[mcp_servers.fs]\ncommand = \"fs\"\n")
	if _, err := MaterializeHarness(f.ctx, "corp", f.gw, f.cat, "codex", f.home, false); err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := toml.Unmarshal([]byte(readFile(t, path)), &doc); err != nil {
		t.Fatal(err)
	}
	if doc["model"] != "gpt-5" || doc["mcp_servers"] == nil {
		t.Errorf("sibling tables lost: %v", doc)
	}
	corp := obj(t, obj(t, doc["model_providers"])["aide-corp"])
	if corp["name"] != "aide-corp" || corp["base_url"] != "https://gw.example.com/v1" || corp["env_key"] != "AIDE_GW_CORP_KEY" || corp["wire_api"] != "responses" {
		t.Errorf("model_providers.corp = %v", corp)
	}
	if doc["model_provider"] != nil {
		t.Errorf("model_provider = %v; aide must not pin Codex's startup provider", doc["model_provider"])
	}
	if got := InspectHarness(f.ctx, "corp", f.gw, f.cat, "codex", f.home); got.State != StateSynced {
		t.Errorf("InspectHarness() = %+v, want synced", got)
	}
	// Codex 0.167 refuses to load wire_api = "chat"; an entry written by an
	// older aide must show as drift so sync rewrites it.
	stale := strings.NewReplacer(`'responses'`, `'chat'`, `"responses"`, `"chat"`).Replace(readFile(t, path))
	writeFile(t, path, stale)
	if got := InspectHarness(f.ctx, "corp", f.gw, f.cat, "codex", f.home); got.State != StateStale || got.Detail != "wire_api differs" {
		t.Errorf("InspectHarness() with wire_api chat = %+v, want stale: wire_api differs", got)
	}
	if _, err := MaterializeHarness(f.ctx, "corp", f.gw, f.cat, "codex", f.home, false); err != nil {
		t.Fatal(err)
	}
	if got := InspectHarness(f.ctx, "corp", f.gw, f.cat, "codex", f.home); got.State != StateSynced {
		t.Errorf("InspectHarness() after resync = %+v, want synced", got)
	}
	if removed, err := RemoveFromHarness(f.ctx, "corp", f.gw, "codex", f.home); err != nil || !removed {
		t.Fatalf("RemoveFromHarness() = %v, %v", removed, err)
	}
	doc = nil
	if err := toml.Unmarshal([]byte(readFile(t, path)), &doc); err != nil {
		t.Fatal(err)
	}
	if doc["model_provider"] != nil || doc["model_providers"] != nil || doc["model"] != "gpt-5" {
		t.Errorf("after remove: %v", doc)
	}
}

func TestMaterialize_CodexKeepsUserProvider(t *testing.T) {
	f := newFixture(t)
	path := filepath.Join(f.dir("codex"), "config.toml")
	writeFile(t, path, "model_provider = \"mine\"\n")
	if _, err := MaterializeHarness(f.ctx, "corp", f.gw, f.cat, "codex", f.home, false); err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := toml.Unmarshal([]byte(readFile(t, path)), &doc); err != nil {
		t.Fatal(err)
	}
	if doc["model_provider"] != "mine" {
		t.Errorf("model_provider = %v, want the user's", doc["model_provider"])
	}
}

func TestInspect_StaleWhenCacheExpired(t *testing.T) {
	f := newFixture(t)
	f.mkdirs(t, "pi")
	if _, err := MaterializeHarness(f.ctx, "corp", f.gw, f.cat, "pi", f.home, false); err != nil {
		t.Fatal(err)
	}
	old := *f.cat
	old.FetchedAt = time.Now().Add(-48 * time.Hour)
	if got := InspectHarness(f.ctx, "corp", f.gw, &old, "pi", f.home); got.State != StateStale {
		t.Errorf("InspectHarness() with an expired cache = %+v, want stale", got)
	}
	if got := InspectHarness(f.ctx, "corp", f.gw, nil, "pi", f.home); got.State != StateStale {
		t.Errorf("InspectHarness() without a cache = %+v, want stale", got)
	}
	more := *f.cat
	more.Models = append(slices.Clone(more.Models), Model{ID: "new"})
	if got := InspectHarness(f.ctx, "corp", f.gw, &more, "pi", f.home); got.State != StateStale {
		t.Errorf("InspectHarness() with new models = %+v, want stale", got)
	}
	if got := InspectHarness(f.ctx, "corp", f.gw, f.cat, "codex", f.home); got.State != StatePending {
		t.Errorf("InspectHarness() without a dir = %+v, want pending", got)
	}
}

func TestInspect_StaleWhenModelMetadataChanges(t *testing.T) {
	f := newFixture(t)
	f.cat.Models = []Model{
		{ID: "custom-pro", DisplayName: "Custom Pro", Provider: "custom", ContextWindow: 262144, MaxOutputTokens: 16384},
	}
	harnesses := []string{"claude", "pi", "omp", "opencode"}
	f.mkdirs(t, harnesses...)
	for _, h := range harnesses {
		if st, err := MaterializeHarness(f.ctx, "corp", f.gw, f.cat, h, f.home, false); err != nil || st.State != StateSynced {
			t.Fatalf("%s MaterializeHarness() = %+v, %v", h, st, err)
		}
	}

	updated := *f.cat
	updated.Models = []Model{
		{ID: "custom-pro", DisplayName: "Custom Pro", Provider: "custom", ContextWindow: 1048576, MaxOutputTokens: 16384},
	}
	for _, h := range harnesses {
		if got := InspectHarness(f.ctx, "corp", f.gw, &updated, h, f.home); got.State != StateStale {
			t.Errorf("%s InspectHarness(updated contextWindow) = %+v, want stale", h, got)
		}
		if st, err := MaterializeHarness(f.ctx, "corp", f.gw, &updated, h, f.home, false); err != nil || st.State != StateSynced {
			t.Fatalf("%s re-MaterializeHarness() = %+v, %v", h, st, err)
		}
		if got := InspectHarness(f.ctx, "corp", f.gw, &updated, h, f.home); got.State != StateSynced {
			t.Errorf("%s InspectHarness(after re-materialize) = %+v, want synced", h, got)
		}
	}

	claudeBody := readFile(t, filepath.Join(f.dir("claude"), "cache", "gateway-models.json"))
	if !strings.Contains(claudeBody, `"corp · 1M context"`) {
		t.Errorf("claude gateway-models.json missing 1M description:\n%s", claudeBody)
	}

	renamed := updated
	renamed.Models = []Model{
		{ID: "custom-pro", DisplayName: "Custom Pro v2", Provider: "custom", ContextWindow: 1048576, MaxOutputTokens: 16384},
	}
	for _, h := range harnesses {
		if got := InspectHarness(f.ctx, "corp", f.gw, &renamed, h, f.home); got.State != StateStale {
			t.Errorf("%s InspectHarness(updated displayName) = %+v, want stale", h, got)
		}
	}

	moreOutput := updated
	moreOutput.Models = []Model{
		{ID: "custom-pro", DisplayName: "Custom Pro", Provider: "custom", ContextWindow: 1048576, MaxOutputTokens: 32768},
	}
	for _, h := range []string{"pi", "omp", "opencode"} {
		if got := InspectHarness(f.ctx, "corp", f.gw, &moreOutput, h, f.home); got.State != StateStale {
			t.Errorf("%s InspectHarness(updated maxOutputTokens) = %+v, want stale", h, got)
		}
	}
}

func TestRemoveFromHarness_MissingFiles(t *testing.T) {
	f := newFixture(t)
	for _, h := range config.DefaultGatewayHarnesses {
		if removed, err := RemoveFromHarness(f.ctx, "corp", f.gw, h, f.home); err != nil || removed {
			t.Errorf("RemoveFromHarness(%s) = %v, %v; want false, nil", h, removed, err)
		}
	}
}

func TestSync(t *testing.T) {
	f := newFixture(t)
	f.mkdirs(t, "claude", "pi")
	srv, _ := modelsServer(t, `{"data":[{"id":"m1","owned_by":"x"},{"id":"m2"}]}`)
	f.gw.BaseURL = srv.URL
	f.gw.Harnesses = nil
	var progress bytes.Buffer
	res, err := Sync(context.Background(), SyncOptions{
		ProfileName: "work", Profile: f.ctx, GatewayName: "corp", Gateway: f.gw,
		Token: testToken, HomeDir: f.home, Progress: &progress,
	})
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if len(res.Catalogue.Models) != 2 || res.Catalogue.Profile != "work" || res.Catalogue.Gateway != "corp" {
		t.Errorf("catalogue = %+v", res.Catalogue)
	}
	if cat, err := ReadCache(f.home, "work", "corp"); err != nil || cat == nil || len(cat.Models) != 2 {
		t.Errorf("cache = %+v, %v", cat, err)
	}
	states := map[string]HarnessSyncState{}
	for _, st := range res.HarnessStatuses {
		states[st.Harness] = st.State
	}
	want := map[string]HarnessSyncState{"claude": StateSynced, "pi": StateSynced, "opencode": StatePending, "codex": StatePending}
	for h, s := range want {
		if states[h] != s {
			t.Errorf("%s state = %q, want %q (all: %+v)", h, states[h], s, res.HarnessStatuses)
		}
	}
	if _, err := os.Stat(f.dir("opencode")); !os.IsNotExist(err) {
		t.Error("Sync created an unprovisioned harness dir")
	}
	if out := progress.String(); !strings.Contains(out, "Fetching models") || !strings.Contains(out, "Cached 2 LLM models") {
		t.Errorf("progress = %q", out)
	}
	if strings.Contains(progress.String(), testToken) {
		t.Error("progress leaks the token")
	}
}

func TestSync_PrefetchSkipsFetch(t *testing.T) {
	f := newFixture(t)
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		http.Error(w, "down", http.StatusBadGateway)
	}))
	defer srv.Close()
	f.gw.BaseURL = srv.URL
	var progress bytes.Buffer
	res, err := Sync(context.Background(), SyncOptions{
		ProfileName: "work", Profile: f.ctx, GatewayName: "corp", Gateway: f.gw,
		HomeDir: f.home, Prefetch: true, Models: []Model{{ID: "m1"}}, Progress: &progress,
	})
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if hits != 0 {
		t.Errorf("FetchModels was called %d times", hits)
	}
	if len(res.Catalogue.Models) != 1 || res.Catalogue.Models[0].ID != "m1" {
		t.Errorf("catalogue = %+v", res.Catalogue)
	}
	if cat, err := ReadCache(f.home, "work", "corp"); err != nil || cat == nil || len(cat.Models) != 1 {
		t.Errorf("cache = %+v, %v", cat, err)
	}
	if strings.Contains(progress.String(), "Fetching models") {
		t.Errorf("prefetch printed a fetch line: %q", progress.String())
	}
	if !strings.Contains(progress.String(), "Cached 1 LLM models") {
		t.Errorf("progress = %q", progress.String())
	}
}

func TestSync_PrefetchNilModels(t *testing.T) {
	f := newFixture(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("FetchModels must not run when Prefetch is set")
		http.Error(w, "down", http.StatusBadGateway)
	}))
	defer srv.Close()
	f.gw.BaseURL = srv.URL
	res, err := Sync(context.Background(), SyncOptions{
		ProfileName: "work", Profile: f.ctx, GatewayName: "corp", Gateway: f.gw,
		HomeDir: f.home, Prefetch: true, Models: nil,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Catalogue.Models) != 0 {
		t.Errorf("models = %+v", res.Catalogue.Models)
	}
}

func TestSync_ClaudeOnlyForDefaultGateway(t *testing.T) {
	f := newFixture(t)
	f.mkdirs(t, "claude", "pi")
	srv, _ := modelsServer(t, `{"data":[{"id":"m1"}]}`)
	alpha := config.GatewayConfig{BaseURL: srv.URL, Store: "work", Key: "alpha_token"}
	beta := config.GatewayConfig{BaseURL: strings.Replace(srv.URL, "127.0.0.1", "localhost", 1), Store: "work", Key: "beta_token"}
	f.ctx.Gateways = map[string]config.GatewayConfig{"alpha": alpha, "beta": beta}

	res, err := Sync(context.Background(), SyncOptions{
		ProfileName: "work", Profile: f.ctx, GatewayName: "beta", Gateway: beta, Token: testToken, HomeDir: f.home,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, st := range res.HarnessStatuses {
		if st.Harness == "claude" && st.State == StateSynced {
			t.Errorf("beta (not claude's default) wrote Claude's file: %+v", st)
		}
		if st.Harness == "pi" && st.State != StateSynced {
			t.Errorf("pi = %+v, want synced", st)
		}
	}
	if _, err := os.Stat(filepath.Join(f.dir("claude"), "cache", "gateway-models.json")); !os.IsNotExist(err) {
		t.Error("gateway-models.json written for a non-default gateway")
	}
}

func TestSync_FetchErrorWritesNothing(t *testing.T) {
	f := newFixture(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "down", http.StatusBadGateway)
	}))
	defer srv.Close()
	f.gw.BaseURL = srv.URL
	_, err := Sync(context.Background(), SyncOptions{
		ProfileName: "work", Profile: f.ctx, GatewayName: "corp", Gateway: f.gw, Token: testToken, HomeDir: f.home,
	})
	if err == nil {
		t.Fatal("Sync() succeeded against a failing gateway")
	}
	if cat, _ := ReadCache(f.home, "work", "corp"); cat != nil {
		t.Error("failed sync wrote a cache")
	}
}

func TestRemoveFromHarness_LeavesOtherURLsEntries(t *testing.T) {
	f := newFixture(t)
	f.gw.API = "openai-responses"
	other := f.gw
	other.BaseURL = "https://someone-else.example.com"
	otherCat := *f.cat
	otherCat.BaseURL = other.BaseURL
	for _, h := range []string{"pi", "opencode", "codex"} {
		f.mkdirs(t, h)
		if _, err := MaterializeHarness(f.ctx, "corp", other, &otherCat, h, f.home, false); err != nil {
			t.Fatal(err)
		}
		removed, err := RemoveFromHarness(f.ctx, "corp", f.gw, h, f.home)
		if err != nil || removed {
			t.Errorf("%s: RemoveFromHarness() = %v, %v; want the other URL's entry kept", h, removed, err)
		}
		if st := InspectHarness(f.ctx, "corp", other, &otherCat, h, f.home); st.State != StateSynced {
			t.Errorf("%s: entry for the other URL was disturbed: %+v", h, st)
		}
	}
}

func TestMaterializeHarness_OpenCodeHonorsOPENCODE_CONFIG(t *testing.T) {
	f := newFixture(t)
	custom := filepath.Join(f.home, "custom-opencode", "my-opencode.jsonc")
	f.ctx.Profile = ""
	f.ctx.Env = map[string]string{"OPENCODE_CONFIG": custom}
	st, err := MaterializeHarness(f.ctx, "corp", f.gw, f.cat, "opencode", f.home, true)
	if err != nil {
		t.Fatal(err)
	}
	if st.FilePath != custom || st.State != StateSynced {
		t.Fatalf("MaterializeHarness() = %+v, want Synced at %s", st, custom)
	}
	if got := InspectHarness(f.ctx, "corp", f.gw, f.cat, "opencode", f.home); got.State != StateSynced || got.FilePath != custom {
		t.Fatalf("InspectHarness() = %+v, want Synced at %s", got, custom)
	}

	// Ambient shell OPENCODE_CONFIG is ignored on unisolated ("default") Context.
	t.Setenv("OPENCODE_CONFIG", filepath.Join(f.home, "ambient-opencode", "opencode.jsonc"))
	unisolated := config.Context{}
	st2 := InspectHarness(unisolated, "corp", f.gw, f.cat, "opencode", f.home)
	wantDefault := filepath.Join(f.home, ".config", "opencode", "opencode.jsonc")
	if st2.FilePath != wantDefault {
		t.Errorf("unisolated InspectHarness FilePath = %q, want %q", st2.FilePath, wantDefault)
	}
}

func TestCanSyncHarness_FollowsClaudeChoice(t *testing.T) {
	ctx := config.Context{Profile: "work", GatewayChoice: map[string]string{"claude": "beta"}}
	if CanSyncHarness(ctx, "alpha", "claude") || !CanSyncHarness(ctx, "beta", "claude") {
		t.Error("claude's gateway file belongs to the chosen gateway only")
	}
	ctx.GatewayChoice = nil
	if CanSyncHarness(ctx, "alpha", "claude") {
		t.Error("an unset choice owns no claude gateway file")
	}
	if !CanSyncHarness(ctx, "alpha", "pi") {
		t.Error("named-entry harnesses take every gateway")
	}
}

func TestInspectHarness_StandbyGatewayNilCacheDoesNotPanic(t *testing.T) {
	f := newFixture(t)
	f.mkdirs(t, "claude")
	standby := config.GatewayConfig{BaseURL: f.gw.BaseURL, Store: "work", Key: "standby_token"}
	f.ctx.Gateways = map[string]config.GatewayConfig{"corp": f.gw, "standby": standby}
	f.ctx.DefaultCredentials = []string{"corp"}

	if _, err := MaterializeHarness(f.ctx, "corp", f.gw, f.cat, "claude", f.home, false); err != nil {
		t.Fatal(err)
	}
	if got := InspectHarness(f.ctx, "standby", standby, nil, "claude", f.home); got.State != StatePending {
		t.Errorf("InspectHarness(standby, nil cache) = %+v, want pending", got)
	}
}

func TestMatchesBaseURL_AndInspectHarnessURLMismatch(t *testing.T) {
	f := newFixture(t)
	f.mkdirs(t, "pi", "claude")
	if !MatchesBaseURL(f.cat, "https://gw.example.com/v1/") {
		t.Fatal("expected normalized URLs to match")
	}
	if MatchesBaseURL(f.cat, "https://other.example.com") || MatchesBaseURL(nil, f.gw.BaseURL) {
		t.Fatal("expected mismatched or nil catalogue to return false")
	}
	if _, err := MaterializeHarness(f.ctx, "corp", f.gw, f.cat, "pi", f.home, false); err != nil {
		t.Fatal(err)
	}
	f.gw.BaseURL = "https://new-gw.example.com"
	st := InspectHarness(f.ctx, "corp", f.gw, f.cat, "pi", f.home)
	if st.State != StateStale || st.Detail != "base URL changed — run: aide gateway sync" {
		t.Fatalf("InspectHarness(pi) = %+v, want stale base URL changed", st)
	}
	// SingleGatewayPerDir standby branch must also not report synced when cat.BaseURL mismatches gw.BaseURL.
	standbyCtx := f.ctx
	standbyCtx.GatewayChoice = map[string]string{"claude": "other"}
	stClaude := InspectHarness(standbyCtx, "corp", f.gw, f.cat, "claude", f.home)
	if stClaude.State == StateSynced {
		t.Fatalf("InspectHarness(claude standby) = %+v, should not be synced on URL mismatch", stClaude)
	}
}

// Entries an earlier aide wrote under the bare gateway name are
// removed, and so are the pins it wrote; an entry at another URL is the
// user's and stays.
func TestMaterialize_RemovesLegacyBareEntries(t *testing.T) {
	f := newFixture(t)
	url := "https://gw.example.com"

	ocPath := filepath.Join(f.dir("opencode"), "opencode.jsonc")
	writeFile(t, ocPath, `{"model":"corp/plain","providers":{"corp":{"settings":{"baseURL":"`+url+`/v1"}},"mine":{"settings":{"baseURL":"https://x"}}}}`)
	piPath := filepath.Join(f.dir("pi"), "models.json")
	writeFile(t, piPath, `{"providers":{"corp":{"baseUrl":"`+url+`"}}}`)
	ompPath := filepath.Join(f.dir("omp"), "models.yml")
	writeFile(t, ompPath, "providers:\n  corp:\n    baseUrl: "+url+"\n")
	cxPath := filepath.Join(f.dir("codex"), "config.toml")
	writeFile(t, cxPath, "model_provider = 'corp'\n[model_providers.corp]\nbase_url = '"+url+"/v1'\nwire_api = 'chat'\n")

	f.ctx.Gateways = map[string]config.GatewayConfig{"corp": f.gw}
	for _, h := range []string{"opencode", "pi", "omp", "codex"} {
		if _, err := MaterializeHarness(f.ctx, "corp", f.gw, f.cat, h, f.home, false); err != nil {
			t.Fatal(err)
		}
		if _, err := Prune(f.ctx, h, f.home, []string{"corp"}); err != nil {
			t.Fatal(err)
		}
	}
	for path, gone := range map[string]string{ocPath: `"model"`, cxPath: "model_provider ="} {
		if body := readFile(t, path); strings.Contains(body, gone) {
			t.Errorf("%s still has the pin aide wrote (%s):\n%s", path, gone, body)
		}
	}
	for _, path := range []string{ocPath, piPath, ompPath} {
		body := readFile(t, path)
		if strings.Contains(body, `"corp"`) || strings.Contains(body, "  corp:") || !strings.Contains(body, "aide-corp") {
			t.Errorf("%s still has a bare corp entry:\n%s", path, body)
		}
	}
	if body := readFile(t, ocPath); !strings.Contains(body, `"mine"`) {
		t.Errorf("user's opencode provider lost:\n%s", body)
	}
	if body := readFile(t, cxPath); strings.Contains(body, "model_providers.corp]") || strings.Contains(body, "chat") {
		t.Errorf("legacy codex entry survived:\n%s", body)
	}
}

// A split-off <gw>-codex leaves no pin behind for the bare <gw> entry.
func TestPrune_CodexSplitDropsBareEntryAndPin(t *testing.T) {
	f := newFixture(t)
	f.gw.API = "openai-responses"
	path := filepath.Join(f.dir("codex"), "config.toml")
	writeFile(t, path, "model_provider = 'corp'\n[model_providers.corp]\nbase_url = 'https://gw.example.com/v1'\n")
	f.ctx.Gateways = map[string]config.GatewayConfig{"corp-codex": f.gw}
	if _, err := MaterializeHarness(f.ctx, "corp-codex", f.gw, f.cat, "codex", f.home, false); err != nil {
		t.Fatal(err)
	}
	if _, err := Prune(f.ctx, "codex", f.home, []string{"corp-codex"}); err != nil {
		t.Fatal(err)
	}
	body := readFile(t, path)
	if strings.Contains(body, "model_provider =") || strings.Contains(body, "model_providers.corp]") || !strings.Contains(body, "aide-corp-codex") {
		t.Errorf("config.toml:\n%s", body)
	}
}

// Prune drops aide-<gw> entries of gateways that are no longer
// configured, and the pins aide wrote, but keeps what the user set.
func TestPrune_StaleEntriesAndUserPins(t *testing.T) {
	f := newFixture(t)
	f.ctx.Gateways = map[string]config.GatewayConfig{"corp": f.gw}
	ocPath := filepath.Join(f.dir("opencode"), "opencode.jsonc")
	writeFile(t, ocPath, `{"model":"anthropic/claude","providers":{"aide-gone":{"settings":{"baseURL":"https://x/v1"}},"aide-corp":{"settings":{"baseURL":"https://gw.example.com/v1"}}}}`)
	piPath := filepath.Join(f.dir("pi"), "models.json")
	writeFile(t, piPath, `{"providers":{"aide-gone":{"baseUrl":"https://x"},"aide-corp":{"baseUrl":"https://gw.example.com"}}}`)
	ompPath := filepath.Join(f.dir("omp"), "models.yml")
	writeFile(t, ompPath, "providers:\n  aide-gone:\n    baseUrl: https://x\n  aide-corp:\n    baseUrl: https://gw.example.com\n")
	cxPath := filepath.Join(f.dir("codex"), "config.toml")
	writeFile(t, cxPath, "model_provider = 'mine'\n[model_providers.aide-gone]\nbase_url = 'https://x/v1'\n[model_providers.aide-corp]\nbase_url = 'https://gw.example.com/v1'\n")
	for _, h := range []string{"opencode", "pi", "omp", "codex"} {
		changed, err := Prune(f.ctx, h, f.home, []string{"corp"})
		if err != nil || !changed {
			t.Fatalf("Prune(%s) = %v, %v; want changed", h, changed, err)
		}
	}
	for _, path := range []string{ocPath, piPath, ompPath, cxPath} {
		body := readFile(t, path)
		if strings.Contains(body, "aide-gone") || !strings.Contains(body, "aide-corp") {
			t.Errorf("%s:\n%s", path, body)
		}
	}
	if !strings.Contains(readFile(t, ocPath), `"anthropic/claude"`) || !strings.Contains(readFile(t, cxPath), "model_provider = 'mine'") {
		t.Error("a pin the user set was removed")
	}
	if changed, err := Prune(f.ctx, "pi", f.home, []string{"corp"}); err != nil || changed {
		t.Errorf("a second Prune = %v, %v; want unchanged", changed, err)
	}
	if changed, err := Prune(f.ctx, "claude", f.home, nil); err != nil || changed {
		t.Errorf("Prune(claude) = %v, %v", changed, err)
	}
}

// A profile that shares the native default dir with other profiles keeps
// their aide-<gw> entries when it prunes, but still drops legacy bare
// entries.
func TestPrune_SharedDefaultDirKeepsOtherProfilesEntries(t *testing.T) {
	f := newFixture(t)
	f.ctx = config.Context{Agent: "opencode", Sandbox: &config.SandboxRef{Disabled: true}}
	f.ctx.Gateways = map[string]config.GatewayConfig{"corp": f.gw}
	path := filepath.Join(f.home, ".config", "opencode", "opencode.jsonc")
	writeFile(t, path, `{"providers":{"aide-other":{"settings":{"baseURL":"https://x/v1"}},"corp":{"settings":{"baseURL":"https://gw.example.com/v1"}}}}`)
	if _, err := Prune(f.ctx, "opencode", f.home, []string{"corp"}); err != nil {
		t.Fatal(err)
	}
	body := readFile(t, path)
	if !strings.Contains(body, "aide-other") || strings.Contains(body, `"corp"`) {
		t.Errorf("opencode.jsonc:\n%s", body)
	}
}

func TestCacheModels_PinnedModel(t *testing.T) {
	home := t.TempDir()
	ctx := config.Context{Agent: "opencode", Sandbox: &config.SandboxRef{Disabled: true}}
	writeFile(t, filepath.Join(home, ".config", "opencode", "opencode.jsonc"), `{"model":"aide-corp/claude-opus-4"}`)
	h := CacheModels(home)
	if got := h.PinnedModel(ctx, "corp"); got != "claude-opus-4" {
		t.Errorf("PinnedModel(corp) = %q", got)
	}
	if got := h.PinnedModel(ctx, "other"); got != "" {
		t.Errorf("PinnedModel(other) = %q, want empty", got)
	}
}
