package launcher

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/mock/gomock"

	"github.com/jskswamy/aide/internal/gateway"
	"github.com/jskswamy/aide/internal/launcher/mocks"
)

const gatewayTestToken = "gw-token-value"

// gatewayServer serves a /v1/models list, counting requests; failing
// makes every request a 502.
type gatewayServer struct {
	*httptest.Server
	hits    atomic.Int32
	failing atomic.Bool
}

func newGatewayServer(t *testing.T, body string) *gatewayServer {
	t.Helper()
	gs := &gatewayServer{}
	gs.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gs.hits.Add(1)
		if gs.failing.Load() || r.Header.Get("Authorization") != "Bearer "+gatewayTestToken {
			http.Error(w, "unavailable", http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(gs.Close)
	return gs
}

// fakeGatewayStore makes every secret store hold the gateway token.
func fakeGatewayStore(t *testing.T) {
	t.Helper()
	prev := loadSecretStore
	loadSecretStore = func(string) (map[string]string, error) {
		return map[string]string{"corp_token": gatewayTestToken, "beta_token": gatewayTestToken}, nil
	}
	t.Cleanup(func() { loadSecretStore = prev })
}

func gatewayConfig(url string) string {
	return fmt.Sprintf(`
profiles:
  work:
    agent: pi
    config_dir_name: work
    gateways:
      corp:
        base_url: %s
        store: work
        key: corp_token
default_profile: work
`, url)
}

// launchGateway launches cfg as agent with HOME=home and returns the
// agent env and stderr.
func launchGateway(t *testing.T, home, cfg, agent string, auth AuthMode, creds ...string) ([]string, string) {
	t.Helper()
	t.Setenv("HOME", home)
	configDir := t.TempDir()
	writeMinimalConfig(t, configDir, cfg)
	ctrl := gomock.NewController(t)
	var env []string
	mockExec := mocks.NewMockExecer(ctrl)
	mockExec.EXPECT().Exec(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ string, _ []string, e []string) error { env = e; return nil }).MaxTimes(1)
	var stderr bytes.Buffer
	l := &Launcher{
		Execer:    mockExec,
		ConfigDir: configDir,
		Stderr:    &stderr,
		Auth:      auth,
		LookPath:  func(file string) (string, error) { return "/usr/local/bin/" + file, nil },
	}
	if len(creds) > 0 {
		l.Credentials, l.CredentialsSet = creds, true
	}
	if err := l.Launch(t.TempDir(), agent, nil, false, false, nil, nil); err != nil {
		t.Fatalf("Launch() error = %v\nstderr:\n%s", err, stderr.String())
	}
	if strings.Contains(stderr.String(), gatewayTestToken) {
		t.Error("stderr contains the gateway token")
	}
	return env, stderr.String()
}

func seedCache(t *testing.T, home, baseURL string, age time.Duration, ids ...string) {
	t.Helper()
	cat := &gateway.CachedCatalogue{Profile: "work", Gateway: "corp", BaseURL: baseURL, FetchedAt: time.Now().Add(-age)}
	for _, id := range ids {
		cat.Models = append(cat.Models, gateway.Model{ID: id, DisplayName: id})
	}
	if err := gateway.WriteCache(home, cat); err != nil {
		t.Fatal(err)
	}
}

// piModelIDs reads providers.corp's model ids from Pi's models.json.
func piModelIDs(t *testing.T, home string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(home, ".pi-work", "models.json"))
	if err != nil {
		t.Fatalf("reading models.json: %v", err)
	}
	if strings.Contains(string(data), gatewayTestToken) {
		t.Error("models.json contains the token")
	}
	var doc struct {
		Providers map[string]struct {
			Models []struct {
				ID string `json:"id"`
			} `json:"models"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, m := range doc.Providers["corp"].Models {
		ids = append(ids, m.ID)
	}
	return ids
}

func TestLauncher_GatewayWarmLaunchMakesNoRequest(t *testing.T) {
	fakeGatewayStore(t)
	gs := newGatewayServer(t, `{"data":[{"id":"fresh"}]}`)
	home := t.TempDir()
	seedCache(t, home, gs.URL, time.Hour, "cached")

	// First launch materializes Pi's file from the fresh cache.
	env, stderr := launchGateway(t, home, gatewayConfig(gs.URL), "", "")
	if got, _ := envValue(env, "CORP_TOKEN"); got != gatewayTestToken {
		t.Errorf("CORP_TOKEN = %q, want the gateway token", got)
	}
	if !strings.Contains(stderr, `materialized 1 cached models for gateway "corp"`) {
		t.Errorf("no materialize notice:\n%s", stderr)
	}
	if ids := piModelIDs(t, home); len(ids) != 1 || ids[0] != "cached" {
		t.Errorf("pi models = %v, want [cached]", ids)
	}

	// Second launch finds it synced and stays quiet.
	_, stderr = launchGateway(t, home, gatewayConfig(gs.URL), "", "")
	if strings.Contains(stderr, "gateway") {
		t.Errorf("warm launch printed gateway output:\n%s", stderr)
	}
	if n := gs.hits.Load(); n != 0 {
		t.Errorf("gateway requests = %d, want 0", n)
	}
}

func TestLauncher_GatewayHarnessSwitchMaterializes(t *testing.T) {
	fakeGatewayStore(t)
	gs := newGatewayServer(t, `{"data":[]}`)
	home := t.TempDir()
	seedCache(t, home, gs.URL, time.Hour, "m1", "m2")

	for _, agent := range []string{"pi", "opencode", "codex", "claude"} {
		_, stderr := launchGateway(t, home, gatewayConfig(gs.URL), agent, "")
		if !strings.Contains(stderr, "materialized 2 cached models") {
			t.Errorf("%s: no materialize notice:\n%s", agent, stderr)
		}
	}
	for _, f := range []string{".pi-work/models.json", ".opencode-work/opencode.jsonc", ".codex-work/config.toml", ".claude-work/cache/gateway-models.json"} {
		if _, err := os.Stat(filepath.Join(home, f)); err != nil {
			t.Errorf("%s not materialized: %v", f, err)
		}
	}
	if n := gs.hits.Load(); n != 0 {
		t.Errorf("gateway requests = %d, want 0", n)
	}
}

func TestLauncher_GatewayExpiredCacheRefreshes(t *testing.T) {
	fakeGatewayStore(t)
	gs := newGatewayServer(t, `{"data":[{"id":"new-a"},{"id":"new-b"}]}`)
	home := t.TempDir()
	seedCache(t, home, gs.URL, 30*time.Hour, "old")

	_, stderr := launchGateway(t, home, gatewayConfig(gs.URL), "", "")
	if !strings.Contains(stderr, `gateway "corp" cache expired (last synced 30h ago, ttl 24h); refreshing models from `+gs.URL+"/v1/models") {
		t.Errorf("no refresh progress:\n%s", stderr)
	}
	if !strings.Contains(stderr, `synced 2 models for gateway "corp" -> ~/.pi-work/models.json`) {
		t.Errorf("no synced line:\n%s", stderr)
	}
	if n := gs.hits.Load(); n != 1 {
		t.Errorf("gateway requests = %d, want 1", n)
	}
	if ids := piModelIDs(t, home); len(ids) != 2 || ids[0] != "new-a" {
		t.Errorf("pi models = %v, want the refreshed list", ids)
	}
	cat, err := gateway.ReadCache(home, "work", "corp")
	if err != nil || cat == nil || time.Since(cat.FetchedAt) > time.Minute {
		t.Errorf("cache not refreshed: %+v, %v", cat, err)
	}
}

func TestLauncher_GatewayOfflineFallsBackToCache(t *testing.T) {
	fakeGatewayStore(t)
	gs := newGatewayServer(t, `{"data":[]}`)
	gs.failing.Store(true)
	home := t.TempDir()
	seedCache(t, home, gs.URL, 30*time.Hour, "cached-a", "cached-b")

	_, stderr := launchGateway(t, home, gatewayConfig(gs.URL), "", "")
	if !strings.Contains(stderr, `aide: warning: could not refresh gateway "corp"`) ||
		!strings.Contains(stderr, "using cached catalogue from 30h ago (2 models)") {
		t.Errorf("no fallback warning:\n%s", stderr)
	}
	if ids := piModelIDs(t, home); len(ids) != 2 {
		t.Errorf("pi models = %v, want the cached list", ids)
	}
}

func TestLauncher_GatewayColdLaunchFailureStillLaunches(t *testing.T) {
	fakeGatewayStore(t)
	gs := newGatewayServer(t, `{"data":[]}`)
	gs.failing.Store(true)
	home := t.TempDir()

	env, stderr := launchGateway(t, home, gatewayConfig(gs.URL), "", "")
	if !strings.Contains(stderr, `aide: warning: could not fetch initial models for gateway "corp"`) {
		t.Errorf("no cold-launch warning:\n%s", stderr)
	}
	if got, _ := envValue(env, "CORP_TOKEN"); got != gatewayTestToken {
		t.Error("launch without a catalogue dropped the token")
	}
}

func TestLauncher_GatewayNativeLoginSkipsSync(t *testing.T) {
	fakeGatewayStore(t)
	gs := newGatewayServer(t, `{"data":[{"id":"m"}]}`)
	home := t.TempDir()
	_, stderr := launchGateway(t, home, gatewayConfig(gs.URL), "", AuthLogin)
	if n := gs.hits.Load(); n != 0 {
		t.Errorf("login launch made %d gateway requests", n)
	}
	if strings.Contains(stderr, "materialized") || strings.Contains(stderr, "refreshing") {
		t.Errorf("login launch synced:\n%s", stderr)
	}
	if _, err := os.Stat(filepath.Join(home, ".pi-work", "models.json")); !os.IsNotExist(err) {
		t.Error("login launch wrote models.json")
	}
}

func TestLauncher_GatewayCredentialSwitchRewritesClaude(t *testing.T) {
	fakeGatewayStore(t)
	gs := newGatewayServer(t, `{"data":[]}`)
	home := t.TempDir()
	betaURL := strings.Replace(gs.URL, "127.0.0.1", "localhost", 1)
	seedCache(t, home, gs.URL, time.Hour, "corp-model")
	beta := &gateway.CachedCatalogue{Profile: "work", Gateway: "gw-beta", BaseURL: betaURL, FetchedAt: time.Now(),
		Models: []gateway.Model{{ID: "beta-model", DisplayName: "beta-model"}}}
	if err := gateway.WriteCache(home, beta); err != nil {
		t.Fatal(err)
	}
	cfg := strings.Replace(gatewayConfig(gs.URL), "default_profile: work", fmt.Sprintf(`      gw-beta:
        base_url: %s
        store: work
        key: beta_token
default_profile: work`, betaURL), 1)

	claudeFile := filepath.Join(home, ".claude-work", "cache", "gateway-models.json")
	env, _ := launchGateway(t, home, cfg, "claude", "")
	if got, _ := envValue(env, "ANTHROPIC_BASE_URL"); got != gs.URL {
		t.Errorf("default launch ANTHROPIC_BASE_URL = %q, want corp's", got)
	}
	if data, _ := os.ReadFile(claudeFile); !strings.Contains(string(data), "corp-model") {
		t.Errorf("default launch wrote %s", data)
	}

	env, _ = launchGateway(t, home, cfg, "claude", "", "gw-beta")
	if got, _ := envValue(env, "ANTHROPIC_BASE_URL"); got != betaURL {
		t.Errorf("--credential gw-beta ANTHROPIC_BASE_URL = %q, want beta's", got)
	}
	if data, _ := os.ReadFile(claudeFile); !strings.Contains(string(data), "beta-model") {
		t.Errorf("--credential gw-beta left %s", data)
	}
}

func TestLauncher_GatewayIgnoresProfileEndpointForOtherHarnesses(t *testing.T) {
	fakeGatewayStore(t)
	gs := newGatewayServer(t, `{"data":[]}`)
	home := t.TempDir()
	seedCache(t, home, gs.URL, time.Hour, "m")
	cfg := strings.Replace(gatewayConfig(gs.URL), "    config_dir_name: work\n",
		"    config_dir_name: work\n    env:\n      ANTHROPIC_BASE_URL: https://proxy.example.com\n", 1)
	env, _ := launchGateway(t, home, cfg, "pi", "")
	if got, _ := envValue(env, "CORP_TOKEN"); got != gatewayTestToken {
		t.Errorf("CORP_TOKEN = %q", got)
	}
}
