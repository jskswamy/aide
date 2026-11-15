package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/gateway"
	"github.com/jskswamy/aide/internal/output"
	"github.com/jskswamy/aide/internal/prefs"
	"github.com/jskswamy/aide/internal/secrets"
	"gopkg.in/yaml.v3"
)

const gwCmdToken = "cli-gateway-token"

// gatewayEnv is an isolated HOME and config dir with profiles work and
// home, the testdata age key, and a gateway serving /v1/models.
type gatewayEnv struct {
	home, xdg string
	srv       *httptest.Server
	body      atomic.Value // the /v1/models response
}

const gatewayCmdConfig = `profiles:
  work:
    agent: claude
    config_dir_name: work
  home:
    agent: pi
    config_dir_name: home
default_profile: work
`

func newGatewayEnv(t *testing.T) *gatewayEnv {
	t.Helper()
	e := &gatewayEnv{home: t.TempDir()}
	e.xdg = filepath.Join(e.home, "xdg")
	t.Setenv("HOME", e.home)
	t.Setenv("XDG_CONFIG_HOME", e.xdg)
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("PATH", "")
	t.Setenv("SOPS_AGE_KEY", "")
	t.Setenv("SOPS_AGE_KEY_FILE", filepath.Join(repoTestdataDir(t), "age-key.txt"))
	t.Chdir(e.home)
	if err := os.MkdirAll(filepath.Join(e.xdg, "aide"), 0o700); err != nil {
		t.Fatal(err)
	}
	e.writeConfig(t, gatewayCmdConfig)
	e.body.Store(`{"data":[{"id":"m1","display_name":"Model One","owned_by":"acme","context_window":200000},{"id":"m2"}]}`)
	e.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+gwCmdToken {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		body, _ := e.body.Load().(string)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(e.srv.Close)
	return e
}

func (e *gatewayEnv) writeConfig(t *testing.T, body string) {
	t.Helper()
	cfgDir := filepath.Join(e.xdg, "aide")
	_ = os.RemoveAll(filepath.Join(cfgDir, "profiles"))
	_ = os.Remove(filepath.Join(cfgDir, "sandbox.yaml"))
	_ = os.Remove(filepath.Join(cfgDir, "provisioning.yaml"))
	cfgPath := filepath.Join(cfgDir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(cfgDir, e.home)
	if err != nil {
		t.Fatal(err)
	}
	cfg.LegacyMonolithic = false
	cfg.NeedsGatewayMigration = false // the helper seeds the split layout; gateways are written in the new form
	cfg.ProfileSchema = true
	if err := config.WriteConfigTo(cfg, cfgPath); err != nil {
		t.Fatal(err)
	}
}

// writeLegacyProfile writes the work profile as the user's own
// profiles/work.yaml, untouched: the writer no longer carries
// default_credentials:, so a test that needs the legacy shapes must not
// round-trip them through it.
func (e *gatewayEnv) writeLegacyProfile(t *testing.T, body string) {
	t.Helper()
	cfgDir := filepath.Join(e.xdg, "aide")
	_ = os.RemoveAll(filepath.Join(cfgDir, "profiles"))
	if err := os.MkdirAll(filepath.Join(cfgDir, "profiles"), 0o700); err != nil {
		t.Fatal(err)
	}
	for f, content := range map[string]string{"config.yaml": "default_profile: work\n", "profiles/work.yaml": body} {
		if err := os.WriteFile(filepath.Join(cfgDir, f), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func (e *gatewayEnv) config(t *testing.T) string {
	t.Helper()
	cfgDir := filepath.Join(e.xdg, "aide")
	cfg, err := config.Load(cfgDir, e.home)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(4)
	if err := enc.Encode(cfg); err != nil {
		t.Fatal(err)
	}
	_ = enc.Close()
	return buf.String()
}

func (e *gatewayEnv) store(t *testing.T, name string) map[string]string {
	t.Helper()
	path := filepath.Join(e.xdg, "aide", "secrets", name+".enc.yaml")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	}
	m, err := secrets.LoadSecretsMap(path)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// runGateway runs `aide gateway <args>` with stdin and returns stdout
// and stderr together.
func runGateway(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()
	cmd := gatewayCmd()
	output.RegisterFlag(cmd)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

func TestGatewayCmd_AddWithTokenStdin(t *testing.T) {
	e := newGatewayEnv(t)
	if err := os.MkdirAll(filepath.Join(e.home, ".pi-work"), 0o700); err != nil {
		t.Fatal(err)
	}
	out, err := runGateway(t, gwCmdToken+"\n", "add", "corp", "--url", e.srv.URL+"/v1/", "--profile", "work", "--token-stdin")
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}
	if strings.Contains(out, gwCmdToken) {
		t.Error("output contains the token")
	}
	for _, want := range []string{
		`Encrypted token into ~/xdg/aide/secrets/work.enc.yaml (key: corp_token)`,
		`Saved gateway "corp" under profile "work"`,
		"Cached 2 LLM models",
		"pi ", "synced",
		"opencode", "pending",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}

	cfg := e.config(t)
	if strings.Contains(cfg, gwCmdToken) || strings.Contains(cfg, "credentials") {
		t.Errorf("config.yaml carries the token or credentials:\n%s", cfg)
	}
	loaded, err := config.Load(filepath.Join(e.xdg, "aide"), e.home)
	if err != nil {
		t.Fatal(err)
	}
	gw := loaded.Contexts["work"].Gateways["corp"]
	want := config.GatewayConfig{BaseURL: e.srv.URL, RefreshTTL: "24h", Store: "work", Key: "corp_token"}
	if gw.BaseURL != want.BaseURL || gw.RefreshTTL != want.RefreshTTL || gw.Store != want.Store || gw.Key != want.Key ||
		strings.Join(gw.Harnesses, ",") != "claude,pi,omp,opencode" || gw.API != "anthropic-messages" {
		t.Errorf("gateways.corp = %+v, want %+v serving every harness but codex", gw, want)
	}
	if cx := loaded.Contexts["work"].Gateways["corp-codex"]; cx.API != "openai-responses" || cx.URL != e.srv.URL+"/v1" || strings.Join(cx.Harnesses, ",") != "codex" {
		t.Errorf("gateways.corp-codex = %+v, want the Responses gateway serving codex", cx)
	}
	if loaded.NeedsGatewayMigration {
		t.Error("add must write the new gateway form")
	}
	if got := e.store(t, "work"); got["corp_token"] != gwCmdToken {
		t.Errorf("secret store = %v", got)
	}
	if cat, err := gateway.ReadCache(e.home, "work", "corp"); err != nil || cat == nil || len(cat.Models) != 2 {
		t.Errorf("cache = %+v, %v", cat, err)
	}
	data, err := os.ReadFile(filepath.Join(e.home, ".pi-work", "models.json"))
	if err != nil || strings.Contains(string(data), gwCmdToken) || !strings.Contains(string(data), "AIDE_GW_CORP_KEY") {
		t.Errorf("pi models.json = %s, %v", data, err)
	}
}

func TestGatewayCmd_AddWithTokenEnvAndDefaults(t *testing.T) {
	e := newGatewayEnv(t)
	t.Setenv("MY_GW_TOKEN", gwCmdToken)
	url := strings.Replace(e.srv.URL, "127.0.0.1", "localhost", 1)
	out, err := runGateway(t, "", "add", "--url", url, "--token-env", "MY_GW_TOKEN",
		"--ttl", "12h", "--harnesses", "pi,codex", "--key", "llm_key", "--store", "gw")
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}
	loaded, err := config.Load(filepath.Join(e.xdg, "aide"), e.home)
	if err != nil {
		t.Fatal(err)
	}
	gw, ok := loaded.Contexts["work"].Gateways["localhost"]
	if !ok {
		t.Fatalf("no gateway named after the host: %+v", loaded.Contexts["work"].Gateways)
	}
	if gw.RefreshTTL != "12h" || gw.Key != "llm_key" || gw.Store != "gw" || strings.Join(gw.Harnesses, ",") != "pi" {
		t.Errorf("gateway = %+v", gw)
	}
	if cx := loaded.Contexts["work"].Gateways["localhost-codex"]; strings.Join(cx.Harnesses, ",") != "codex" {
		t.Errorf("localhost-codex = %+v, want the codex sibling", cx)
	}
	if got := e.store(t, "gw"); got["llm_key"] != gwCmdToken {
		t.Errorf("secret store = %v", got)
	}
}

func TestGatewayCmd_AddPromptsForURLOnTTY(t *testing.T) {
	e := newGatewayEnv(t)
	t.Setenv("MY_GW_TOKEN", gwCmdToken)
	orig := stdinIsTTY
	stdinIsTTY = func() bool { return true }
	t.Cleanup(func() { stdinIsTTY = orig })

	url := strings.Replace(e.srv.URL, "127.0.0.1", "localhost", 1)
	out, err := runGateway(t, url+"\n", "add", "--token-env", "MY_GW_TOKEN")
	if err != nil {
		t.Fatalf("add with prompted URL: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Gateway base URL") {
		t.Errorf("output missing URL prompt:\n%s", out)
	}
	loaded, err := config.Load(filepath.Join(e.xdg, "aide"), e.home)
	if err != nil {
		t.Fatal(err)
	}
	if gw, ok := loaded.Contexts["work"].Gateways["localhost"]; !ok || gw.BaseURL != url {
		t.Errorf("gateways.localhost = %+v, ok=%v, want BaseURL %s", gw, ok, url)
	}
}

func TestGatewayCmd_AddRejectsBadInput(t *testing.T) {
	e := newGatewayEnv(t)
	t.Setenv("MY_GW_TOKEN", gwCmdToken)
	cases := map[string]struct {
		args []string
		want string
	}{
		"missing url non-tty":     {[]string{"add", "corp", "--token-env", "MY_GW_TOKEN"}, "--url is required"},
		"missing url token-stdin": {[]string{"add", "corp", "--token-stdin"}, "--url is required"},
		"both token sources":      {[]string{"add", "corp", "--url", e.srv.URL, "--token-stdin", "--token-env", "MY_GW_TOKEN"}, "mutually exclusive"},
		"empty token env":         {[]string{"add", "corp", "--url", e.srv.URL, "--token-env", "UNSET_GW_TOKEN"}, "UNSET_GW_TOKEN"},
		"no token source":         {[]string{"add", "corp", "--url", e.srv.URL}, "from-env"},
		"remote http":             {[]string{"add", "corp", "--url", "http://gw.example.com", "--token-env", "MY_GW_TOKEN"}, "https"},
		"unknown profile":         {[]string{"add", "corp", "--url", e.srv.URL, "--token-env", "MY_GW_TOKEN", "--profile", "nope"}, "nope"},
		"bad harness":             {[]string{"add", "corp", "--url", e.srv.URL, "--token-env", "MY_GW_TOKEN", "--harnesses", "gemini"}, "gemini"},
		"dangerous key":           {[]string{"add", "corp", "--url", e.srv.URL, "--token-env", "MY_GW_TOKEN", "--key", "ld_preload"}, "LD_PRELOAD"},
	}
	for label, tc := range cases {
		t.Run(label, func(t *testing.T) {
			out, err := runGateway(t, "", tc.args...)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want one mentioning %q\n%s", err, tc.want, out)
			}
			if strings.Contains(e.config(t), "gateways") {
				t.Error("a rejected add wrote config")
			}
			if e.store(t, "work") != nil {
				t.Error("a rejected add wrote the secret store")
			}
		})
	}
}

func openAnonModels(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestGatewayCmd_AddNoAuthProbe(t *testing.T) {
	e := newGatewayEnv(t)
	srv := openAnonModels(t, `{"data":[{"id":"m1"}]}`)
	if err := os.MkdirAll(filepath.Join(e.home, ".pi-work"), 0o700); err != nil {
		t.Fatal(err)
	}
	out, err := runGateway(t, "", "add", "ollama", "--url", srv.URL, "--profile", "work")
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}
	if strings.Contains(out, "Encrypted token") {
		t.Errorf("encrypted a token:\n%s", out)
	}
	loaded, err := config.Load(filepath.Join(e.xdg, "aide"), e.home)
	if err != nil {
		t.Fatal(err)
	}
	gw := loaded.Contexts["work"].Gateways["ollama"]
	if gw.Store != "" || gw.Key != "" || gw.BaseURL != srv.URL {
		t.Errorf("gateways.ollama = %+v", gw)
	}
	if e.store(t, "work") != nil {
		t.Error("secret store written")
	}
	if cat, err := gateway.ReadCache(e.home, "work", "ollama"); err != nil || cat == nil || len(cat.Models) != 1 {
		t.Errorf("cache = %+v, %v", cat, err)
	}
}

func addHarnessesAnon(t *testing.T) (*gatewayEnv, *httptest.Server) {
	t.Helper()
	e := newGatewayEnv(t)
	srv := openAnonModels(t, `{"data":[{"id":"m1"}]}`)
	if err := os.MkdirAll(filepath.Join(e.home, ".pi-work"), 0o700); err != nil {
		t.Fatal(err)
	}
	return e, srv
}

func stubStdinTTY(t *testing.T) {
	t.Helper()
	orig := stdinIsTTY
	stdinIsTTY = func() bool { return true }
	t.Cleanup(func() { stdinIsTTY = orig })
}

func loadedGateway(t *testing.T, e *gatewayEnv, name string) config.GatewayConfig {
	t.Helper()
	loaded, err := config.Load(filepath.Join(e.xdg, "aide"), e.home)
	if err != nil {
		t.Fatal(err)
	}
	return loaded.Contexts["work"].Gateways[name]
}

func TestGatewayCmd_AddHarnessesNonTTYPrintsDefault(t *testing.T) {
	e, srv := addHarnessesAnon(t)
	out, err := runGateway(t, "", "add", "ollama", "--url", srv.URL, "--profile", "work")
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}
	want := "Harnesses: claude, pi, omp, opencode, codex  (pass --harnesses to change)"
	if !strings.Contains(out, want) {
		t.Errorf("output missing %q:\n%s", want, out)
	}
	if gw := loadedGateway(t, e, "ollama"); !slices.Equal(gw.Harnesses, []string{"claude", "pi", "omp", "opencode"}) {
		t.Errorf("Harnesses = %v, want every harness but codex (its own gateway, ollama-codex, serves it)", gw.Harnesses)
	}
	if cx := loadedGateway(t, e, "ollama-codex"); !slices.Equal(cx.Harnesses, []string{"codex"}) {
		t.Errorf("ollama-codex Harnesses = %v, want [codex]", cx.Harnesses)
	}
}

func TestGatewayCmd_AddHarnessesTTYEnterTakesDefault(t *testing.T) {
	e, srv := addHarnessesAnon(t)
	stubStdinTTY(t)
	out, err := runGateway(t, "\n", "add", "ollama", "--url", srv.URL, "--profile", "work")
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Harnesses [claude,pi,omp,opencode,codex]:") {
		t.Errorf("output missing TTY prompt:\n%s", out)
	}
	if gw := loadedGateway(t, e, "ollama"); !slices.Equal(gw.Harnesses, []string{"claude", "pi", "omp", "opencode"}) {
		t.Errorf("Harnesses = %v, want every harness but codex (its own gateway, ollama-codex, serves it)", gw.Harnesses)
	}
	if cx := loadedGateway(t, e, "ollama-codex"); !slices.Equal(cx.Harnesses, []string{"codex"}) {
		t.Errorf("ollama-codex Harnesses = %v, want [codex]", cx.Harnesses)
	}
}

func TestGatewayCmd_AddHarnessesTTYSubset(t *testing.T) {
	e, srv := addHarnessesAnon(t)
	stubStdinTTY(t)
	out, err := runGateway(t, "pi,opencode\n", "add", "ollama", "--url", srv.URL, "--profile", "work")
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}
	gw := loadedGateway(t, e, "ollama")
	if !slices.Equal(gw.Harnesses, []string{"pi", "opencode"}) {
		t.Errorf("Harnesses = %v, want [pi opencode]", gw.Harnesses)
	}
}

func TestGatewayCmd_AddHarnessesTTYGeminiSavesNothing(t *testing.T) {
	e, srv := addHarnessesAnon(t)
	stubStdinTTY(t)
	out, err := runGateway(t, "gemini\n", "add", "ollama", "--url", srv.URL, "--profile", "work")
	if err == nil || !strings.Contains(err.Error(), "gemini") {
		t.Fatalf("err = %v, want one mentioning gemini\n%s", err, out)
	}
	if strings.Contains(e.config(t), "gateways") {
		t.Errorf("saved a gateway:\n%s", e.config(t))
	}
}

func TestGatewayCmd_AddHarnessesTTYTypedDefault(t *testing.T) {
	e, srv := addHarnessesAnon(t)
	stubStdinTTY(t)
	out, err := runGateway(t, "claude,pi,omp,opencode,codex\n", "add", "ollama", "--url", srv.URL, "--profile", "work")
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Harnesses [claude,pi,omp,opencode,codex]:") {
		t.Errorf("output missing TTY prompt:\n%s", out)
	}
	if gw := loadedGateway(t, e, "ollama"); !slices.Equal(gw.Harnesses, []string{"claude", "pi", "omp", "opencode"}) {
		t.Errorf("Harnesses = %v, want every harness but codex (its own gateway, ollama-codex, serves it)", gw.Harnesses)
	}
	if cx := loadedGateway(t, e, "ollama-codex"); !slices.Equal(cx.Harnesses, []string{"codex"}) {
		t.Errorf("ollama-codex Harnesses = %v, want [codex]", cx.Harnesses)
	}
}

func TestGatewayCmd_AddHarnessesTTYSpacesStillSubset(t *testing.T) {
	e, srv := addHarnessesAnon(t)
	stubStdinTTY(t)
	out, err := runGateway(t, " pi , opencode \n", "add", "ollama", "--url", srv.URL, "--profile", "work")
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}
	gw := loadedGateway(t, e, "ollama")
	if !slices.Equal(gw.Harnesses, []string{"pi", "opencode"}) {
		t.Errorf("Harnesses = %v, want [pi opencode]", gw.Harnesses)
	}
}

func TestGatewayCmd_AddHarnessesFlagSkipsPrompt(t *testing.T) {
	e, srv := addHarnessesAnon(t)
	out, err := runGateway(t, "", "add", "ollama", "--url", srv.URL, "--profile", "work", "--harnesses", "pi")
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}
	if strings.Contains(out, "Harnesses: claude, pi") {
		t.Errorf("printed default list despite --harnesses:\n%s", out)
	}
	gw := loadedGateway(t, e, "ollama")
	if !slices.Equal(gw.Harnesses, []string{"pi"}) {
		t.Errorf("Harnesses = %v, want [pi]", gw.Harnesses)
	}
}

func TestGatewayCmd_AddHarnessesFlagAll(t *testing.T) {
	e, srv := addHarnessesAnon(t)
	out, err := runGateway(t, "", "add", "ollama", "--url", srv.URL, "--profile", "work", "--harnesses", "all")
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}
	if gw := loadedGateway(t, e, "ollama"); !slices.Equal(gw.Harnesses, []string{"claude", "pi", "omp", "opencode"}) {
		t.Errorf("Harnesses = %v, want every harness but codex (its own gateway, ollama-codex, serves it)", gw.Harnesses)
	}
	if cx := loadedGateway(t, e, "ollama-codex"); !slices.Equal(cx.Harnesses, []string{"codex"}) {
		t.Errorf("ollama-codex Harnesses = %v, want [codex]", cx.Harnesses)
	}
}

func TestGatewayCmd_AddHarnessesTokenStdinPrintsNotPrompt(t *testing.T) {
	e := newGatewayEnv(t)
	if err := os.MkdirAll(filepath.Join(e.home, ".pi-work"), 0o700); err != nil {
		t.Fatal(err)
	}
	stubStdinTTY(t)
	out, err := runGateway(t, gwCmdToken+"\n", "add", "corp", "--url", e.srv.URL, "--profile", "work", "--token-stdin")
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Harnesses: claude, pi, omp, opencode, codex  (pass --harnesses to change)") {
		t.Errorf("output missing non-TTY harnesses line:\n%s", out)
	}
	if strings.Contains(out, "Harnesses [claude,pi,omp,opencode,codex]:") {
		t.Errorf("prompted for harnesses with --token-stdin:\n%s", out)
	}
	if got := e.store(t, "work"); got["corp_token"] != gwCmdToken {
		t.Errorf("secret store = %v; token was consumed as harnesses", got)
	}
}

func TestGatewayCmd_AddNoAuthEmptyCatalogue(t *testing.T) {
	e := newGatewayEnv(t)
	hits := atomic.Int32{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	t.Cleanup(srv.Close)
	out, err := runGateway(t, "", "add", "ollama", "--url", srv.URL, "--profile", "work")
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}
	if hits.Load() != 1 {
		t.Errorf("fetches = %d, want 1", hits.Load())
	}
	if e.store(t, "work") != nil {
		t.Error("secret store written")
	}
}

func TestGatewayCmd_AddProbe401NonTTY(t *testing.T) {
	e := newGatewayEnv(t)
	out, err := runGateway(t, "", "add", "corp", "--url", e.srv.URL, "--profile", "work")
	if err == nil || !strings.Contains(err.Error(), "from-env") {
		t.Fatalf("err = %v\n%s", err, out)
	}
	if strings.Contains(e.config(t), "gateways") {
		t.Error("wrote config")
	}
}

func TestGatewayCmd_AddTokenEnvStillUsedWhenProbeWouldSucceed(t *testing.T) {
	e := newGatewayEnv(t)
	srv := openAnonModels(t, `{"data":[{"id":"m1"}]}`)
	t.Setenv("MY_GW_TOKEN", gwCmdToken)
	out, err := runGateway(t, "", "add", "corp", "--url", srv.URL, "--profile", "work", "--token-env", "MY_GW_TOKEN")
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}
	if e.store(t, "work")["corp_token"] != gwCmdToken {
		t.Errorf("store = %v", e.store(t, "work"))
	}
}

func TestGatewayCmd_AddTTY401ThenToken(t *testing.T) {
	e := newGatewayEnv(t)
	var auths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auths = append(auths, r.Header.Get("Authorization"))
		if r.Header.Get("Authorization") != "Bearer "+gwCmdToken {
			http.Error(w, "nope", http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"m1"}]}`))
	}))
	t.Cleanup(srv.Close)
	orig := stdinIsTTY
	stdinIsTTY = func() bool { return true }
	t.Cleanup(func() { stdinIsTTY = orig })
	out, err := runGateway(t, "\n"+gwCmdToken+"\n", "add", "corp", "--url", srv.URL, "--profile", "work")
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}
	if len(auths) < 2 || auths[0] != "" || auths[1] != "Bearer "+gwCmdToken {
		t.Errorf("Authorization log = %v", auths)
	}
	if e.store(t, "work")["corp_token"] != gwCmdToken {
		t.Errorf("store = %v", e.store(t, "work"))
	}
}

func TestGatewayCmd_AddTTY403ThenAbort(t *testing.T) {
	e := newGatewayEnv(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)
	orig := stdinIsTTY
	stdinIsTTY = func() bool { return true }
	t.Cleanup(func() { stdinIsTTY = orig })
	out, err := runGateway(t, "\nbad-token\n\n", "add", "corp", "--url", srv.URL, "--profile", "work")
	if err == nil || !strings.Contains(err.Error(), "aborted") {
		t.Fatalf("err = %v\n%s", err, out)
	}
	if strings.Contains(e.config(t), "gateways") || e.store(t, "work") != nil {
		t.Error("wrote config or secrets")
	}
}

func TestGatewayCmd_AddUnauthenticated403TTY(t *testing.T) {
	e := newGatewayEnv(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)
	orig := stdinIsTTY
	stdinIsTTY = func() bool { return true }
	t.Cleanup(func() { stdinIsTTY = orig })
	out, err := runGateway(t, "\n", "add", "corp", "--url", srv.URL, "--profile", "work")
	if err == nil || !strings.Contains(err.Error(), "aborted") {
		t.Fatalf("err = %v\n%s", err, out)
	}
	if !strings.Contains(out, "Gateway bearer token") {
		t.Errorf("expected token prompt in output, got:\n%s", out)
	}
	if strings.Contains(e.config(t), "gateways") {
		t.Error("wrote config")
	}
}

func TestGatewayCmd_AddTokenEnv403WritesNothing(t *testing.T) {
	e := newGatewayEnv(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("MY_GW_TOKEN", gwCmdToken)
	out, err := runGateway(t, "", "add", "corp", "--url", srv.URL, "--profile", "work", "--token-env", "MY_GW_TOKEN")
	if err == nil {
		t.Fatalf("err = nil\n%s", out)
	}
	if strings.Contains(e.config(t), "gateways") || e.store(t, "work") != nil {
		t.Error("wrote config or secrets")
	}
}

func TestGatewayCmd_SyncNoAuth(t *testing.T) {
	e := newGatewayEnv(t)
	var sawAuth atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			sawAuth.Store(true)
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"m2"}]}`))
	}))
	t.Cleanup(srv.Close)
	if _, err := runGateway(t, "", "add", "ollama", "--url", srv.URL, "--profile", "work"); err != nil {
		t.Fatal(err)
	}
	sawAuth.Store(false)
	out, err := runGateway(t, "", "sync", "ollama", "--profile", "work")
	if err != nil {
		t.Fatalf("sync: %v\n%s", err, out)
	}
	if sawAuth.Load() {
		t.Error("sync sent Authorization")
	}
	if e.store(t, "work") != nil {
		t.Error("sync wrote a secret")
	}
}

func TestGatewayCmd_RemoveNoAuthSkipsSecret(t *testing.T) {
	e := newGatewayEnv(t)
	srv := openAnonModels(t, `{"data":[{"id":"m1"}]}`)
	if _, err := runGateway(t, "", "add", "ollama", "--url", srv.URL, "--profile", "work"); err != nil {
		t.Fatal(err)
	}
	out, err := runGateway(t, "", "remove", "ollama", "--profile", "work")
	if err != nil {
		t.Fatalf("remove: %v\n%s", err, out)
	}
	if strings.Contains(out, "Deleted secret") {
		t.Errorf("deleted a secret:\n%s", out)
	}
	if gw := loadedGateway(t, e, "ollama"); gw.BaseURL != "" {
		t.Error("gateway still in config")
	}
	if gw := loadedGateway(t, e, "ollama-codex"); gw.BaseURL == "" {
		t.Error("remove must leave the split-off ollama-codex gateway in place")
	}
}

func TestGatewayCmd_List(t *testing.T) {
	e := newGatewayEnv(t)
	if _, err := runGateway(t, gwCmdToken, "add", "corp", "--url", e.srv.URL, "--key-stdin", "--default-for", "claude"); err != nil {
		t.Fatal(err)
	}
	out, err := runGateway(t, "", "list", "--profile", "work", "--models")
	if err != nil {
		t.Fatalf("list: %v\n%s", err, out)
	}
	for _, want := range []string{
		"PROFILE: work", "GATEWAY: corp", e.srv.URL, "store=work, key=corp_token", "2 models",
		"ANTHROPIC_AUTH_TOKEN", "AIDE_GW_CORP_KEY", "pending", "Model One", "acme",
		"Chosen for:   claude (profile work)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("list output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, gwCmdToken) {
		t.Error("list printed the token")
	}

	out, err = runGateway(t, "", "list", "--format", "json", "--models")
	if err != nil {
		t.Fatalf("list json: %v\n%s", err, out)
	}
	var entries []GatewayListEntry
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("list json: %v\n%s", err, out)
	}
	if len(entries) != 2 || entries[0].Profile != "work" || entries[0].Gateway != "corp" || entries[1].Gateway != "corp-codex" ||
		entries[0].ModelCount != 2 || len(entries[0].Models) != 2 || len(entries[0].Harnesses) != 4 || entries[0].Expired ||
		len(entries[0].ChosenFor) == 0 || entries[0].ChosenFor[0] != (GatewayChoice{Harness: "claude", Source: "profile work"}) {
		t.Errorf("entries = %+v", entries)
	}
}

func TestGatewayCmd_SyncForcesRefresh(t *testing.T) {
	e := newGatewayEnv(t)
	if _, err := runGateway(t, gwCmdToken, "add", "corp", "--url", e.srv.URL, "--token-stdin"); err != nil {
		t.Fatal(err)
	}
	e.body.Store(`{"data":[{"id":"m1"},{"id":"m2"},{"id":"m3"}]}`)
	out, err := runGateway(t, "", "sync", "--profile", "work")
	if err != nil {
		t.Fatalf("sync: %v\n%s", err, out)
	}
	if !strings.Contains(out, `Syncing gateway "corp" (profile "work")`) || !strings.Contains(out, "Cached 3 LLM models") {
		t.Errorf("sync output:\n%s", out)
	}
	if cat, _ := gateway.ReadCache(e.home, "work", "corp"); cat == nil || len(cat.Models) != 3 {
		t.Errorf("cache after sync = %+v", cat)
	}
	if _, err := runGateway(t, "", "sync", "nope", "--profile", "work"); err == nil {
		t.Error("sync of an unknown gateway succeeded")
	}
}

func TestGatewayCmd_Remove(t *testing.T) {
	e := newGatewayEnv(t)
	if err := os.MkdirAll(filepath.Join(e.home, ".pi-work"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := runGateway(t, gwCmdToken, "add", "corp", "--url", e.srv.URL, "--token-stdin"); err != nil {
		t.Fatal(err)
	}
	if _, err := runGateway(t, gwCmdToken, "add", "other", "--url", e.srv.URL, "--token-stdin", "--key", "other_token"); err != nil {
		t.Fatal(err)
	}
	out, err := runGateway(t, "", "remove", "corp")
	if err != nil {
		t.Fatalf("remove: %v\n%s", err, out)
	}
	for _, want := range []string{`Removed gateway "corp"`, `Kept secret key "corp_token"`, "Removed cache", `removed provider "corp"`} {
		if !strings.Contains(out, want) {
			t.Errorf("remove output missing %q:\n%s", want, out)
		}
	}
	// The split-off corp-codex still uses the key; removing it frees it.
	out, err = runGateway(t, "", "remove", "corp-codex")
	if err != nil || !strings.Contains(out, `Deleted secret key "corp_token"`) {
		t.Fatalf("remove corp-codex: %v\n%s", err, out)
	}
	cfg := e.config(t)
	if strings.Contains(cfg, "corp") || !strings.Contains(cfg, "other") {
		t.Errorf("config after remove:\n%s", cfg)
	}
	if got := e.store(t, "work"); got["corp_token"] != "" || got["other_token"] != gwCmdToken {
		t.Errorf("store after remove = %v", got)
	}
	if cat, _ := gateway.ReadCache(e.home, "work", "corp"); cat != nil {
		t.Error("cache survived remove")
	}
	if data, _ := os.ReadFile(filepath.Join(e.home, ".pi-work", "models.json")); strings.Contains(string(data), `"corp"`) {
		t.Errorf("pi provider survived remove: %s", data)
	}
	if _, err := runGateway(t, "", "remove", "corp"); err == nil {
		t.Error("removing a missing gateway succeeded")
	}
}

// Removing a gateway clears the choices that named it, in the profile
// and for repositories, and says so.
func TestGatewayCmd_RemoveClearsChoicesWithNotice(t *testing.T) {
	e := newGatewayEnv(t)
	if _, err := runGateway(t, gwCmdToken, "add", "corp", "--url", e.srv.URL, "--token-stdin", "--harnesses", "claude,pi,omp,opencode", "--default-for", "claude", "--default-for", "pi"); err != nil {
		t.Fatal(err)
	}
	prefPath := prefs.DefaultPath(e.home)
	pf := &prefs.File{Version: prefs.Version}
	key := prefs.Key{Profile: "work", Remote: "github.com/org/repo"}
	pf.Set(key, prefs.Preference{Gateway: map[string]string{"opencode": "corp", "claude": "none"}})
	if err := prefs.Save(prefPath, pf); err != nil {
		t.Fatal(err)
	}

	out, err := runGateway(t, "", "remove", "corp")
	if err != nil {
		t.Fatalf("remove: %v\n%s", err, out)
	}
	for _, want := range []string{
		"Cleared the gateway choice for claude, pi on profile \"work\"",
		"Cleared the repository's gateway choice for opencode (github.com/org/repo)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("remove output missing %q:\n%s", want, out)
		}
	}
	loaded, err := config.Load(filepath.Join(e.xdg, "aide"), e.home)
	if err != nil {
		t.Fatal(err)
	}
	for h, v := range loaded.Contexts["work"].GatewayChoice {
		if v == "corp" {
			t.Errorf("gateway_choice[%s] still names the removed gateway", h)
		}
	}
	got, err := prefs.Load(prefPath)
	if err != nil {
		t.Fatal(err)
	}
	if p, _ := got.Get(key); p.Gateway["opencode"] != "" || p.Gateway["claude"] != "none" {
		t.Errorf("repository preference after remove = %+v, want only the unrelated choice kept", p)
	}
}

// A provider credential becomes a gateway, so gateway use takes it like
// any other.
func TestGatewayCmd_UseAcceptsMigratedCredential(t *testing.T) {
	e := newGatewayEnv(t)
	e.writeLegacyProfile(t, `agent: claude
config_dir_name: work
credentials:
  openrouter: {backend: age, store: work, key: openrouter_api_key}
default_credentials: [openrouter]
`)
	out, err := runGateway(t, "", "use", "openrouter", "--harness", "pi", "--profile", "work")
	if err != nil || !strings.Contains(out, `Set the gateway for pi on profile "work" to "openrouter"`) {
		t.Fatalf("use openrouter: %v\n%s", err, out)
	}
	loaded, err := config.Load(filepath.Join(e.xdg, "aide"), e.home)
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.Contexts["work"].GatewayChoice["pi"]; got != "openrouter" {
		t.Errorf("gateway_choice[pi] = %q, want openrouter", got)
	}
	if _, err := runGateway(t, "", "use", "openrouter", "--harness", "claude", "--profile", "work"); err == nil {
		t.Error("openrouter is not marked for claude, but use accepted it")
	}
}

func TestGatewayCmd_RemoveKeepsSharedSecret(t *testing.T) {
	e := newGatewayEnv(t)
	if _, err := runGateway(t, gwCmdToken, "add", "corp", "--url", e.srv.URL, "--token-stdin", "--profile", "work", "--store", "shared"); err != nil {
		t.Fatal(err)
	}
	if _, err := runGateway(t, gwCmdToken, "add", "corp", "--url", e.srv.URL, "--token-stdin", "--profile", "home", "--store", "shared"); err != nil {
		t.Fatal(err)
	}
	out, err := runGateway(t, "", "remove", "corp", "--profile", "work")
	if err != nil {
		t.Fatalf("remove: %v\n%s", err, out)
	}
	if !strings.Contains(out, "still used") {
		t.Errorf("remove did not explain keeping the secret:\n%s", out)
	}
	if got := e.store(t, "shared"); got["corp_token"] != gwCmdToken {
		t.Errorf("shared secret deleted: %v", got)
	}

	out, err = runGateway(t, "", "remove", "corp", "--profile", "home", "--keep-secret")
	if err != nil {
		t.Fatalf("remove --keep-secret: %v\n%s", err, out)
	}
	if got := e.store(t, "shared"); got["corp_token"] != gwCmdToken {
		t.Errorf("--keep-secret deleted the secret: %v", got)
	}
}

func TestGatewayCmd_AddRefusesToReplaceAnotherGateway(t *testing.T) {
	e := newGatewayEnv(t)
	if _, err := runGateway(t, gwCmdToken, "add", "corp", "--url", e.srv.URL, "--token-stdin"); err != nil {
		t.Fatal(err)
	}
	// Same URL: rotating the token is allowed.
	if out, err := runGateway(t, gwCmdToken, "add", "corp", "--url", e.srv.URL, "--token-stdin"); err != nil {
		t.Fatalf("re-add with the same URL: %v\n%s", err, out)
	}
	other := strings.Replace(e.srv.URL, "127.0.0.1", "localhost", 1)
	_, err := runGateway(t, "other-token", "add", "corp", "--url", other, "--token-stdin")
	if err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("replacing corp with another URL: err = %v, want a --force hint", err)
	}
	if got := e.store(t, "work"); got["corp_token"] != gwCmdToken {
		t.Errorf("refused add replaced the token: %v", got)
	}
	if out, err := runGateway(t, gwCmdToken, "add", "corp", "--url", other, "--token-stdin", "--force"); err != nil {
		t.Fatalf("add --force: %v\n%s", err, out)
	}
}

func TestGatewayCmd_AddRefusesForeignSecretKey(t *testing.T) {
	e := newGatewayEnv(t)
	secretsDir := filepath.Join(e.xdg, "aide", "secrets")
	if err := secrets.NewManager(secretsDir, "").SetKey("work", secretsDir, "corp_token", "used-elsewhere", ""); err != nil {
		t.Fatal(err)
	}
	_, err := runGateway(t, gwCmdToken, "add", "corp", "--url", e.srv.URL, "--token-stdin")
	if err == nil || !strings.Contains(err.Error(), "corp_token") || !strings.Contains(err.Error(), "--key") {
		t.Fatalf("err = %v, want refusal naming the key and --key", err)
	}
	if got := e.store(t, "work"); got["corp_token"] != "used-elsewhere" {
		t.Errorf("existing key overwritten: %v", got)
	}
}

func TestGatewayCmd_RemoveKeepsSecretUsedByEnvTemplate(t *testing.T) {
	e := newGatewayEnv(t)
	if _, err := runGateway(t, gwCmdToken, "add", "corp", "--url", e.srv.URL, "--token-stdin"); err != nil {
		t.Fatal(err)
	}
	cfg := strings.Replace(e.config(t), "        agent: pi\n",
		"        agent: pi\n        secret: work\n        env:\n            CORP: '{{ .secrets.corp_token }}'\n", 1)
	if !strings.Contains(cfg, "CORP:") {
		t.Fatalf("could not seed env template:\n%s", cfg)
	}
	e.writeConfig(t, cfg)
	out, err := runGateway(t, "", "remove", "corp")
	if err != nil {
		t.Fatalf("remove: %v\n%s", err, out)
	}
	if got := e.store(t, "work"); got["corp_token"] != gwCmdToken {
		t.Errorf("secret used by an env template was deleted: %v\n%s", got, out)
	}
}

func TestGatewayCmd_UseAndListWithBuiltinCredential(t *testing.T) {
	e := newGatewayEnv(t)
	if err := os.MkdirAll(filepath.Join(e.home, ".claude-work"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(e.home, ".codex-work"), 0o700); err != nil {
		t.Fatal(err)
	}
	e.writeLegacyProfile(t, `agent: claude
config_dir_name: work
credentials:
  anthropic:
    backend: age
    store: work
    key: anthropic_api_key
default_credentials: [anthropic]
`)
	if _, err := runGateway(t, gwCmdToken, "add", "api", "--url", e.srv.URL, "--token-stdin", "--profile", "work"); err != nil {
		t.Fatal(err)
	}

	// List shows the migrated anthropic credential as the default gateway, and api-codex as a standby (codex keeps its own login).
	out, err := runGateway(t, "", "list", "--profile", "work")
	if err != nil {
		t.Fatalf("list: %v\n%s", err, out)
	}
	for _, want := range []string{
		"● GATEWAY: anthropic  [ACTIVE — default for: claude, pi, omp, opencode]",
		"URL:          https://api.anthropic.com",
		"○ GATEWAY: api-codex  [standby — switch: aide gateway use api-codex --harness <h> --profile work, or launch: aide --gateway api-codex]",
		"○ GATEWAY: api  [standby — switch: aide gateway use api --harness <h> --profile work, or launch: aide --gateway api]",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("list missing %q:\n%s", want, out)
		}
	}

	// Choose api for claude via `aide gateway use api --harness claude`.
	out, err = runGateway(t, "", "use", "api", "--harness", "claude", "--profile", "work")
	if err != nil {
		t.Fatalf("use api: %v\n%s", err, out)
	}
	if !strings.Contains(out, `Set the gateway for claude on profile "work" to "api"`) {
		t.Errorf("unexpected use output:\n%s", out)
	}
	loaded, err := config.Load(filepath.Join(e.xdg, "aide"), e.home)
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.Contexts["work"].GatewayChoice; got["claude"] != "api" || got["codex"] != "none" {
		t.Errorf("GatewayChoice after use api = %v", got)
	}
	if _, err := runGateway(t, "", "use", "ghost", "--harness", "claude", "--profile", "work"); err == nil {
		t.Error("use of an unknown gateway succeeded")
	}
	if _, err := runGateway(t, "", "use", "api", "--profile", "work"); err == nil {
		t.Error("use without --harness succeeded")
	}
	if out, err = runGateway(t, "", "use", "none", "--harness", "claude", "--profile", "work"); err != nil {
		t.Fatalf("use none: %v\n%s", err, out)
	}
	if out, err = runGateway(t, "", "use", "--unset", "--harness", "claude", "--profile", "work"); err != nil {
		t.Fatalf("use --unset: %v\n%s", err, out)
	}
	if loaded, err = config.Load(filepath.Join(e.xdg, "aide"), e.home); err != nil {
		t.Fatal(err)
	}
	if _, set := loaded.Contexts["work"].GatewayChoice["claude"]; set {
		t.Errorf("--unset left %v", loaded.Contexts["work"].GatewayChoice)
	}
	for _, h := range []string{"claude", "pi", "omp", "opencode"} {
		if _, err := runGateway(t, "", "use", "api", "--harness", h, "--profile", "work"); err != nil {
			t.Fatal(err)
		}
	}

	// Verify `aide gateway list` (with no --profile flag) defaults to the active profile ("work"),
	// and sorts active "api" ahead of standby "anthropic".
	out, err = runGateway(t, "", "list")
	if err != nil {
		t.Fatalf("list default profile: %v\n%s", err, out)
	}
	apiIdx := strings.Index(out, "● GATEWAY: api  [ACTIVE — default for: claude, pi, omp, opencode]")
	anthIdx := strings.Index(out, "○ GATEWAY: anthropic  [standby — switch: aide gateway use anthropic --harness <h> --profile work, or launch: aide --gateway anthropic]")
	if apiIdx < 0 || anthIdx < 0 || apiIdx > anthIdx {
		t.Errorf("expected active api before standby anthropic (apiIdx=%d, anthIdx=%d):\n%s", apiIdx, anthIdx, out)
	}
	if _, err := runGateway(t, "", "list", "--profile", "work", "--all"); err == nil {
		t.Error("expected --profile and --all to be mutually exclusive")
	}

	// The interactive picker lists the harness's gateways, then none, with nothing preselected.
	orig := stdinIsTTY
	stdinIsTTY = func() bool { return true }
	t.Cleanup(func() { stdinIsTTY = orig })
	if _, err := runGateway(t, "\n", "use", "--harness", "claude", "--profile", "work"); err == nil {
		t.Error("an empty answer selected a gateway")
	}
	out, err = runGateway(t, "3\n", "use", "--harness", "claude", "--profile", "work")
	if err != nil {
		t.Fatalf("interactive use: %v\n%s", err, out)
	}
	if !strings.Contains(out, `to "none"`) {
		t.Errorf("unexpected interactive use output:\n%s", out)
	}
}

func TestGatewayCmd_AddInteractiveNameAndDefaultPrompt(t *testing.T) {
	e := newGatewayEnv(t)
	e.writeLegacyProfile(t, `agent: claude
config_dir_name: work
credentials:
  anthropic:
    backend: age
    store: work
    key: anthropic_api_key
default_credentials: [anthropic]
`)
	orig := stdinIsTTY
	stdinIsTTY = func() bool { return true }
	t.Cleanup(func() { stdinIsTTY = orig })

	url := strings.Replace(e.srv.URL, "127.0.0.1", "localhost", 1)
	stdin := strings.Join([]string{url, "acme", "", gwCmdToken, "y", "y"}, "\n") + "\n"
	out, err := runGateway(t, stdin, "add", "--profile", "work")
	if err != nil {
		t.Fatalf("interactive add: %v\n%s", err, out)
	}
	for _, want := range []string{
		"Gateway base URL",
		"Gateway name [localhost]: ",
		"Make acme the default for claude in profile work? [y/N]: ",
		"Make acme-codex the default for codex in profile work? [y/N]: ",
		`Saved gateway "acme" under profile "work"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("interactive add output missing %q:\n%s", want, out)
		}
	}
	loaded, err := config.Load(filepath.Join(e.xdg, "aide"), e.home)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := loaded.Contexts["work"].Gateways["acme"]; !ok {
		t.Errorf("gateways = %+v, want acme", loaded.Contexts["work"].Gateways)
	}
	if got := loaded.Contexts["work"].GatewayChoice; got["claude"] != "acme" || got["codex"] != "acme-codex" || got["pi"] != "anthropic" {
		t.Errorf("GatewayChoice = %v, want claude=acme, codex=acme-codex, pi=anthropic (migrated default)", got)
	}
}

func TestGatewayCmd_AddPromptsForNameWhenURLFlagPassedOnTTY(t *testing.T) {
	e := newGatewayEnv(t)
	orig := stdinIsTTY
	stdinIsTTY = func() bool { return true }
	t.Cleanup(func() { stdinIsTTY = orig })

	url := strings.Replace(e.srv.URL, "127.0.0.1", "localhost", 1)
	stdin := strings.Join([]string{"acme", "", gwCmdToken}, "\n") + "\n"
	out, err := runGateway(t, stdin, "add", "--url", url, "--profile", "work")
	if err != nil {
		t.Fatalf("add with --url on TTY: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Gateway name [localhost]: ") {
		t.Errorf("expected Gateway name prompt even when --url was passed via flag:\n%s", out)
	}
	loaded, err := config.Load(filepath.Join(e.xdg, "aide"), e.home)
	if err != nil {
		t.Fatal(err)
	}
	if gw, ok := loaded.Contexts["work"].Gateways["acme"]; !ok || gw.Key != "acme_token" {
		t.Errorf("gateways.acme = %+v, ok=%v", gw, ok)
	}
}

func TestGatewayCmd_RenameMigratesSecretKeyCacheAndHarnesses(t *testing.T) {
	e := newGatewayEnv(t)
	for _, dir := range []string{".claude-work", ".pi-work", ".opencode-work", ".codex-work"} {
		if err := os.MkdirAll(filepath.Join(e.home, dir), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := runGateway(t, gwCmdToken, "add", "api", "--url", e.srv.URL, "--default", "--token-stdin", "--harnesses", "claude,pi,omp,opencode", "--profile", "work"); err != nil {
		t.Fatal(err)
	}

	// Seed a repository preference referencing "api".
	prefPath := prefs.DefaultPath(e.home)
	pf := &prefs.File{Version: prefs.Version}
	pf.Set(prefs.Key{Profile: "work", Remote: "github.com/org/repo"}, prefs.Preference{
		LegacyCredentials: prefs.Selection("api"),
	})
	if err := prefs.Save(prefPath, pf); err != nil {
		t.Fatal(err)
	}

	out, err := runGateway(t, "", "edit", "api", "--name", "acme", "--profile", "work")
	if err != nil {
		t.Fatalf("rename api acme: %v\n%s", err, out)
	}
	for _, want := range []string{
		`Migrated secret key "api_token" → "acme_token"`,
		`Renamed gateway "api" → "acme" under profile "work"`,
		`Moved cache`,
		`env: AIDE_GW_ACME_KEY`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rename output missing %q:\n%s", want, out)
		}
	}

	// Verify config.yaml and default_credentials updated.
	loaded, err := config.Load(filepath.Join(e.xdg, "aide"), e.home)
	if err != nil {
		t.Fatal(err)
	}
	wctx := loaded.Contexts["work"]
	if _, exists := wctx.Gateways["api"]; exists {
		t.Error("old gateway 'api' still present in config")
	}
	if gw, ok := wctx.Gateways["acme"]; !ok || gw.Key != "acme_token" {
		t.Errorf("gateways.acme = %+v, ok=%v", gw, ok)
	}
	if wctx.GatewayChoice["claude"] != "acme" || wctx.GatewayChoice["pi"] != "acme" {
		t.Errorf("GatewayChoice = %v, want acme", wctx.GatewayChoice)
	}

	// Verify secret key migrated and old key deleted.
	sec := e.store(t, "work")
	if _, hasOld := sec["api_token"]; hasOld {
		t.Errorf("secret store still has old key api_token: %v", sec)
	}
	if sec["acme_token"] != gwCmdToken {
		t.Errorf("secret store acme_token = %q, want %q", sec["acme_token"], gwCmdToken)
	}

	// Verify cache moved.
	if cat, _ := gateway.ReadCache(e.home, "work", "api"); cat != nil {
		t.Error("old cache file for 'api' still exists")
	}
	if cat, _ := gateway.ReadCache(e.home, "work", "acme"); cat == nil || cat.Gateway != "acme" {
		t.Errorf("new cache for 'acme' = %+v", cat)
	}

	// Verify pi, opencode, and codex files cleaned up "api" and have "acme".
	piRaw, err := os.ReadFile(filepath.Join(e.home, ".pi-work", "models.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(piRaw), `"$API_TOKEN"`) || !strings.Contains(string(piRaw), `"aide-acme"`) || !strings.Contains(string(piRaw), `"$AIDE_GW_ACME_KEY"`) {
		t.Errorf("pi models.json after rename:\n%s", piRaw)
	}
	// Verify repository preference updated to "acme".
	updatedPrefs, err := prefs.Load(prefPath)
	if err != nil {
		t.Fatal(err)
	}
	if p, ok := updatedPrefs.Get(prefs.Key{Profile: "work", Remote: "github.com/org/repo"}); !ok || p.LegacyCredentials == nil || (*p.LegacyCredentials)[0] != "acme" {
		t.Errorf("repo preference after rename = %+v, ok=%v", p, ok)
	}
}

func TestGatewayCmd_EditInteractiveAndTokenRotation(t *testing.T) {
	var currentToken atomic.Value
	currentToken.Store(gwCmdToken)

	e := newGatewayEnv(t)
	rotSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := currentToken.Load().(string)
		if !ok {
			t.Error("current gateway token is not a string")
			http.Error(w, "invalid test token", http.StatusInternalServerError)
			return
		}
		want := "Bearer " + token
		if r.Header.Get("Authorization") != want {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"m1","display_name":"Model One","owned_by":"acme"}]}`))
	}))
	t.Cleanup(rotSrv.Close)

	for _, dir := range []string{".pi-work", ".opencode-work"} {
		if err := os.MkdirAll(filepath.Join(e.home, dir), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := runGateway(t, gwCmdToken, "add", "corp", "--url", rotSrv.URL, "--token-stdin", "--harnesses", "claude,pi,omp,opencode", "--profile", "work"); err != nil {
		t.Fatal(err)
	}

	// 1. Flag-driven token rotation via --token-env and harness narrowing (--harnesses claude,pi).
	const rotatedToken = "rotated-secret-token-2"
	currentToken.Store(rotatedToken)
	t.Setenv("NEW_CORP_TOKEN", rotatedToken)
	out, err := runGateway(t, "", "edit", "corp", "--token-env", "NEW_CORP_TOKEN", "--harnesses", "claude,pi", "--ttl", "12h", "--profile", "work")
	if err != nil {
		t.Fatalf("edit token-env + harnesses: %v\n%s", err, out)
	}
	if got := e.store(t, "work")["corp_token"]; got != rotatedToken {
		t.Errorf("rotated token in secret store = %q, want %q", got, rotatedToken)
	}
	// opencode provider entry should have been removed since harnesses narrowed to claude,pi.
	ocRaw, err := os.ReadFile(filepath.Join(e.home, ".opencode-work", "opencode.jsonc"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(ocRaw), "corp") {
		t.Errorf("opencode.jsonc still contains corp after narrowing --harnesses to claude,pi:\n%s", ocRaw)
	}

	// 2. Interactive TTY edit: rename corp -> acme, keep URL/TTL, reset harnesses to "all", no token rotation.
	orig := stdinIsTTY
	stdinIsTTY = func() bool { return true }
	t.Cleanup(func() { stdinIsTTY = orig })

	stdin := strings.Join([]string{"acme", "", "", "all", "n"}, "\n") + "\n"
	out, err = runGateway(t, stdin, "edit", "--profile", "work")
	if err != nil {
		t.Fatalf("interactive edit: %v\n%s", err, out)
	}
	if !strings.Contains(out, `Renamed gateway "corp" → "acme"`) {
		t.Errorf("interactive edit output:\n%s", out)
	}
	loaded, err := config.Load(filepath.Join(e.xdg, "aide"), e.home)
	if err != nil {
		t.Fatal(err)
	}
	gw, ok := loaded.Contexts["work"].Gateways["acme"]
	if !ok || gw.RefreshTTL != "12h" || len(gw.Harnesses) != 0 || gw.Key != "acme_token" {
		t.Errorf("edited gateway acme = %+v, ok=%v", gw, ok)
	}
}

func TestGatewayCmd_ListDefaultProfileVsAllAndGatewayCompletion(t *testing.T) {
	e := newGatewayEnv(t)
	if _, err := runGateway(t, gwCmdToken, "add", "work-gw", "--url", e.srv.URL, "--token-stdin", "--harnesses", "claude,pi,omp,opencode", "--profile", "work"); err != nil {
		t.Fatal(err)
	}
	if _, err := runGateway(t, gwCmdToken, "add", "home-gw", "--url", e.srv.URL, "--token-stdin", "--harnesses", "claude,pi,omp,opencode", "--profile", "home"); err != nil {
		t.Fatal(err)
	}

	// Default `aide gateway list` resolves only the active profile ("work") from CWD.
	out, err := runGateway(t, "", "list")
	if err != nil {
		t.Fatalf("list default: %v\n%s", err, out)
	}
	if !strings.Contains(out, "PROFILE: work") || !strings.Contains(out, "work-gw") || strings.Contains(out, "PROFILE: home") {
		t.Errorf("default list should include only work profile:\n%s", out)
	}

	// `aide gateway list --all` lists both profiles.
	out, err = runGateway(t, "", "list", "--all")
	if err != nil {
		t.Fatalf("list --all: %v\n%s", err, out)
	}
	if !strings.Contains(out, "PROFILE: home") || !strings.Contains(out, "home-gw") || !strings.Contains(out, "PROFILE: work") || !strings.Contains(out, "work-gw") {
		t.Errorf("list --all should include both home and work profiles:\n%s", out)
	}

	// Verify `--gateway` completion returns synthesized gateways on the active profile.
	wCmd := whichCmd()
	gotComp := credentialNamesForCompletion(wCmd)
	if !slices.Contains(gotComp, "work-gw") {
		t.Errorf("credentialNamesForCompletion = %v, want work-gw included", gotComp)
	}
}

func TestGatewayCmd_PersistentProfileFlagAndPrecedence(t *testing.T) {
	e := newGatewayEnv(t)
	gwRoot := gatewayCmd()
	if gwRoot.PersistentFlags().Lookup("profile") == nil {
		t.Fatal("expected --profile to be registered as a PersistentFlag on gatewayCmd()")
	}
	for _, sub := range gwRoot.Commands() {
		if sub.LocalNonPersistentFlags().Lookup("profile") != nil {
			t.Fatalf("subcommand %q defines a local --profile flag shadowing persistent --profile", sub.Name())
		}
	}
	if _, err := runGateway(t, gwCmdToken, "add", "work-gw", "--url", e.srv.URL, "--token-stdin", "--harnesses", "claude,pi,omp,opencode", "--profile", "work"); err != nil {
		t.Fatal(err)
	}
	if _, err := runGateway(t, gwCmdToken, "add", "home-gw", "--url", e.srv.URL, "--token-stdin", "--harnesses", "claude,pi,omp,opencode", "--profile", "home"); err != nil {
		t.Fatal(err)
	}

	// Both `aide gateway --profile home list` and `aide gateway list --profile home` target `home` even when CWD resolves to `work`.
	for _, args := range [][]string{
		{"--profile", "home", "list"},
		{"list", "--profile", "home"},
	} {
		out, err := runGateway(t, "", args...)
		if err != nil {
			t.Fatalf("%v error: %v\n%s", args, err, out)
		}
		if !strings.Contains(out, "PROFILE: home") || !strings.Contains(out, "home-gw") || strings.Contains(out, "PROFILE: work") {
			t.Errorf("%v output should only list home profile:\n%s", args, out)
		}
	}

	// `aide gateway edit --profile work home-gw` must fail and never fall back to `home`.
	if out, err := runGateway(t, "", "edit", "--profile", "work", "home-gw", "--ttl", "6h"); err == nil || !strings.Contains(err.Error(), `profile "work" has no gateway "home-gw"`) {
		t.Fatalf("edit --profile work home-gw = %v (%s), want profile \"work\" has no gateway \"home-gw\"", err, out)
	}
}

func TestGatewayCmd_EditCrossProfileLookupAndPicker(t *testing.T) {
	e := newGatewayEnv(t)
	e.writeConfig(t, `profiles:
  acme:
    agent: claude
    config_dir_name: acme
  work:
    agent: claude
    config_dir_name: work
default_profile: acme
`)
	if _, err := runGateway(t, gwCmdToken, "add", "acme-gw", "--url", e.srv.URL, "--token-stdin", "--harnesses", "claude,pi,omp,opencode", "--profile", "acme"); err != nil {
		t.Fatal(err)
	}
	if _, err := runGateway(t, gwCmdToken, "add", "work-gw", "--url", e.srv.URL, "--token-stdin", "--harnesses", "claude,pi,omp,opencode", "--profile", "work"); err != nil {
		t.Fatal(err)
	}

	// 1. `aide gateway edit work-gw --ttl 6h` (no --profile) resolves `work-gw` on `work` even though CWD defaults to `acme`.
	out, err := runGateway(t, "", "edit", "work-gw", "--ttl", "6h")
	if err != nil {
		t.Fatalf("cross-profile edit work-gw: %v\n%s", err, out)
	}
	loaded, err := config.Load(filepath.Join(e.xdg, "aide"), e.home)
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.Contexts["work"].Gateways["work-gw"].RefreshTTL; got != "6h" {
		t.Errorf("work/work-gw RefreshTTL = %q, want 6h", got)
	}
	if got := loaded.Contexts["acme"].Gateways["acme-gw"].RefreshTTL; got != "24h" {
		t.Errorf("acme/acme-gw RefreshTTL = %q, want 24h (untouched)", got)
	}

	// 2. Interactive `aide gateway edit` (no <name>, no --profile) shows (profile, gateway, base_url) picker; selecting [2] edits work/work-gw.
	orig := stdinIsTTY
	stdinIsTTY = func() bool { return true }
	t.Cleanup(func() { stdinIsTTY = orig })

	stdin := strings.Join([]string{"2", "", "", "8h", "", "n"}, "\n") + "\n"
	out, err = runGateway(t, stdin, "edit")
	if err != nil {
		t.Fatalf("interactive cross-profile picker edit: %v\n%s", err, out)
	}
	for _, want := range []string{
		"Configured gateways:",
		"[1] acme / acme-gw",
		"[2] work / work-gw",
		`Editing gateway "work-gw" on profile "work"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("picker edit output missing %q:\n%s", want, out)
		}
	}
	loaded, err = config.Load(filepath.Join(e.xdg, "aide"), e.home)
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.Contexts["work"].Gateways["work-gw"].RefreshTTL; got != "8h" {
		t.Errorf("work/work-gw RefreshTTL after picker = %q, want 8h", got)
	}

	// 3. Interactive `aide gateway edit --profile work` (single gateway on `work`) prints the target banner before prompting.
	stdinSingle := strings.Join([]string{"", "", "10h", "", "n"}, "\n") + "\n"
	out, err = runGateway(t, stdinSingle, "edit", "--profile", "work")
	if err != nil {
		t.Fatalf("interactive single-gateway edit: %v\n%s", err, out)
	}
	if !strings.Contains(out, `Editing gateway "work-gw" on profile "work"`) {
		t.Errorf("single-gateway interactive edit missing banner:\n%s", out)
	}
}

func TestGatewayCmd_EditRotateTokenAndURLPromptMatrix(t *testing.T) {
	var currentToken atomic.Value
	currentToken.Store(gwCmdToken)

	e := newGatewayEnv(t)
	newSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := currentToken.Load().(string)
		if !ok {
			t.Error("current gateway token is not a string")
			http.Error(w, "invalid test token", http.StatusInternalServerError)
			return
		}
		want := "Bearer " + token
		if r.Header.Get("Authorization") != want {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"new-m1","display_name":"New Model One","owned_by":"acme"}]}`))
	}))
	t.Cleanup(newSrv.Close)

	if _, err := runGateway(t, gwCmdToken, "add", "work-gw", "--url", e.srv.URL, "--token-stdin", "--harnesses", "claude,pi,omp,opencode", "--profile", "work"); err != nil {
		t.Fatal(err)
	}

	orig := stdinIsTTY
	stdinIsTTY = func() bool { return true }
	t.Cleanup(func() { stdinIsTTY = orig })

	// 1. `--rotate-token` without field flags on a TTY prompts for fields AND rotates token without re-asking `Rotate bearer token? [y/N]: `.
	const token1 = "rotated-tty-token-1"
	currentToken.Store(token1)
	stdinRotate := strings.Join([]string{"", newSrv.URL, "4h", "", token1}, "\n") + "\n"
	out, err := runGateway(t, stdinRotate, "edit", "work-gw", "--profile", "work", "--rotate-token")
	if err != nil {
		t.Fatalf("edit --rotate-token: %v\n%s", err, out)
	}
	if strings.Contains(out, "Rotate bearer token? [y/N]:") {
		t.Errorf("--rotate-token should not ask Rotate bearer token? [y/N]:\n%s", out)
	}
	if !strings.Contains(out, "Gateway base URL [") {
		t.Errorf("--rotate-token without field flags should prompt for fields:\n%s", out)
	}
	if got := e.store(t, "work")["work_gw_token"]; got != token1 {
		t.Errorf("secret after --rotate-token = %q, want %q", got, token1)
	}

	// 2. `--url <new-url>` on a TTY prompts `Rotate bearer token? [y/N]: y` and reads the new token.
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := currentToken.Load().(string)
		if !ok {
			t.Error("current gateway token is not a string")
			http.Error(w, "invalid test token", http.StatusInternalServerError)
			return
		}
		want := "Bearer " + token
		if r.Header.Get("Authorization") != want {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"srv2-m1"}]}`))
	}))
	t.Cleanup(srv2.Close)

	const token2 = "rotated-url-token-2"
	currentToken.Store(token2)
	stdinURL := strings.Join([]string{"y", token2}, "\n") + "\n"
	out, err = runGateway(t, stdinURL, "edit", "work-gw", "--profile", "work", "--url", srv2.URL)
	if err != nil {
		t.Fatalf("edit --url with token prompt: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Rotate bearer token? [y/N]:") {
		t.Errorf("edit --url on TTY should prompt Rotate bearer token? [y/N]:\n%s", out)
	}
	if got := e.store(t, "work")["work_gw_token"]; got != token2 {
		t.Errorf("secret after --url token prompt = %q, want %q", got, token2)
	}

	// 3. `--token-stdin` without field flags rotates token non-interactively without prompting for fields.
	stdinIsTTY = func() bool { return false }
	const token3 = "rotated-stdin-token-3"
	currentToken.Store(token3)
	out, err = runGateway(t, token3+"\n", "edit", "work-gw", "--profile", "work", "--token-stdin")
	if err != nil {
		t.Fatalf("edit --token-stdin: %v\n%s", err, out)
	}
	if strings.Contains(out, "Gateway base URL [") {
		t.Errorf("--token-stdin should not prompt for fields:\n%s", out)
	}
	if got := e.store(t, "work")["work_gw_token"]; got != token3 {
		t.Errorf("secret after --token-stdin = %q, want %q", got, token3)
	}
}

func TestGatewayCmd_EditPreSaveVerificationAndURLMismatchCache(t *testing.T) {
	e := newGatewayEnv(t)
	if err := os.MkdirAll(filepath.Join(e.home, ".pi-work"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := runGateway(t, gwCmdToken, "add", "work-gw", "--url", e.srv.URL, "--token-stdin", "--harnesses", "claude,pi,omp,opencode", "--profile", "work"); err != nil {
		t.Fatal(err)
	}

	unauthSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "invalid token", http.StatusUnauthorized)
	}))
	t.Cleanup(unauthSrv.Close)

	// 1. Pre-save verification rejects a 401 URL change before saving config.yaml, secrets, or cache.
	out, err := runGateway(t, "", "edit", "work-gw", "--profile", "work", "--url", unauthSrv.URL)
	if err == nil || !strings.Contains(err.Error(), `verifying gateway "work-gw"`) {
		t.Fatalf("edit with 401 URL = %v (%s), want verifying gateway error", err, out)
	}
	loaded, err := config.Load(filepath.Join(e.xdg, "aide"), e.home)
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.Contexts["work"].Gateways["work-gw"].BaseURL; got != gateway.NormalizeBaseURL(e.srv.URL) {
		t.Errorf("BaseURL after failed edit = %q, want unchanged %q", got, e.srv.URL)
	}
	if cat, _ := gateway.ReadCache(e.home, "work", "work-gw"); cat == nil || !gateway.MatchesBaseURL(cat, e.srv.URL) {
		t.Errorf("cache after failed edit = %+v, want intact cache for %s", cat, e.srv.URL)
	}

	// 2. When config.yaml has a manually changed base_url that mismatches the cache file:
	//    `gateway list` reports base URL changed with ModelCount == 0, and `gateway use` refuses to materialize stale URL cache.
	cfgText := strings.ReplaceAll(e.config(t), gateway.NormalizeBaseURL(e.srv.URL), "https://manual-override.example.com")
	e.writeConfig(t, cfgText)

	listOut, err := runGateway(t, "", "list", "--profile", "work")
	if err != nil {
		t.Fatalf("list after manual URL change: %v\n%s", err, listOut)
	}
	for _, want := range []string{
		"Cache:        base URL changed (run: aide gateway sync work-gw)",
		"stale (base URL changed — run: aide gateway sync)",
	} {
		if !strings.Contains(listOut, want) {
			t.Errorf("list output missing %q:\n%s", want, listOut)
		}
	}

	jsonOut, err := runGateway(t, "", "list", "--profile", "work", "--format", "json")
	if err != nil {
		t.Fatalf("list json after manual URL change: %v\n%s", err, jsonOut)
	}
	var entries []GatewayListEntry
	if err := json.Unmarshal([]byte(jsonOut), &entries); err != nil {
		t.Fatalf("unmarshal list json: %v\n%s", err, jsonOut)
	}
	if len(entries) != 1 || entries[0].ModelCount != 0 || entries[0].FetchedAt != nil || !entries[0].Expired {
		t.Errorf("JSON list entry on URL mismatch = %+v, want ModelCount=0, FetchedAt=nil, Expired=true", entries)
	}

	// Remove pi models.json and verify `gateway use` does not materialize the mismatched cache.
	piModels := filepath.Join(e.home, ".pi-work", "models.json")
	_ = os.Remove(piModels)
	if _, err := runGateway(t, "", "use", "work-gw", "--harness", "pi", "--profile", "work"); err != nil {
		t.Fatalf("gateway use on mismatched cache: %v", err)
	}
	if _, err := os.Stat(piModels); !os.IsNotExist(err) {
		t.Errorf("gateway use materialized models.json from mismatched cache URL")
	}
}

func TestGatewayCmd_EditFixPassCWDOrderingAmbiguousTTYAndStaleCacheMetadataEdit(t *testing.T) {
	e := newGatewayEnv(t)
	if err := os.MkdirAll(filepath.Join(e.home, ".pi-work"), 0o700); err != nil {
		t.Fatal(err)
	}
	e.writeConfig(t, `profiles:
  alpha:
    agent: claude
    config_dir_name: alpha
  beta:
    agent: claude
    config_dir_name: beta
  work:
    agent: claude
    config_dir_name: work
default_profile: work
`)
	if _, err := runGateway(t, gwCmdToken, "add", "alpha-gw", "--url", e.srv.URL, "--token-stdin", "--harnesses", "claude,pi,omp,opencode", "--profile", "alpha"); err != nil {
		t.Fatal(err)
	}
	if _, err := runGateway(t, gwCmdToken, "add", "work-gw", "--url", e.srv.URL, "--token-stdin", "--harnesses", "claude,pi,omp,opencode", "--profile", "work"); err != nil {
		t.Fatal(err)
	}
	if _, err := runGateway(t, gwCmdToken, "add", "shared", "--url", e.srv.URL, "--token-stdin", "--harnesses", "claude,pi,omp,opencode", "--profile", "alpha"); err != nil {
		t.Fatal(err)
	}
	if _, err := runGateway(t, gwCmdToken, "add", "shared", "--url", e.srv.URL, "--token-stdin", "--harnesses", "claude,pi,omp,opencode", "--profile", "beta"); err != nil {
		t.Fatal(err)
	}

	orig := stdinIsTTY
	stdinIsTTY = func() bool { return true }
	t.Cleanup(func() { stdinIsTTY = orig })

	// 1. CWD-resolved profile ("work") appears as [1] ahead of alphabetically earlier "alpha".
	stdinPicker := strings.Join([]string{"1", "", "", "5h", "", "n"}, "\n") + "\n"
	out, err := runGateway(t, stdinPicker, "edit")
	if err != nil {
		t.Fatalf("picker edit: %v\n%s", err, out)
	}
	if !strings.Contains(out, "[1] work / work-gw") {
		t.Errorf("expected CWD profile 'work' gateway first at [1]:\n%s", out)
	}

	// 2. Ambiguous <name> ("shared" on alpha and beta, neither is CWD profile "work") prompts on TTY.
	out, err = runGateway(t, "2\n", "edit", "shared", "--ttl", "7h")
	if err != nil {
		t.Fatalf("ambiguous edit on TTY: %v\n%s", err, out)
	}
	if !strings.Contains(out, `Gateway "shared" exists on multiple profiles:`) || !strings.Contains(out, "Select profile to edit [1]:") {
		t.Errorf("expected multi-profile disambiguation prompt:\n%s", out)
	}
	loaded, err := config.Load(filepath.Join(e.xdg, "aide"), e.home)
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.Contexts["beta"].Gateways["shared"].RefreshTTL; got != "7h" {
		t.Errorf("beta/shared RefreshTTL = %q, want 7h", got)
	}

	// 3. Metadata-only edit (--ttl) when config.yaml has a manually changed base_url must NOT overwrite oldCat.BaseURL or materialize stale models.
	cfgText := strings.ReplaceAll(e.config(t), gateway.NormalizeBaseURL(e.srv.URL), "https://manual-new.example.com")
	e.writeConfig(t, cfgText)
	piModels := filepath.Join(e.home, ".pi-work", "models.json")
	_ = os.Remove(piModels)

	if _, err := runGateway(t, "", "edit", "work-gw", "--profile", "work", "--ttl", "9h"); err != nil {
		t.Fatalf("metadata edit on mismatched cache: %v", err)
	}
	if cat, _ := gateway.ReadCache(e.home, "work", "work-gw"); cat != nil && gateway.MatchesBaseURL(cat, "https://manual-new.example.com") {
		t.Errorf("metadata edit overwrote stale cache BaseURL to match new URL: %+v", cat)
	}
	if _, err := os.Stat(piModels); !os.IsNotExist(err) {
		t.Errorf("metadata edit materialized pi models.json from mismatched cache BaseURL")
	}
}

// readOnlyConfig makes the next config write fail while the secrets dir
// stays writable, so a command fails after it has stored the token.
func (e *gatewayEnv) readOnlyConfig(t *testing.T) {
	t.Helper()
	cfgDir := filepath.Join(e.xdg, "aide")
	dirs := []string{cfgDir, filepath.Join(cfgDir, "profiles")}
	for _, d := range dirs {
		if err := os.Chmod(d, 0o500); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		for _, d := range dirs {
			_ = os.Chmod(d, 0o700)
		}
	})
}

func TestGatewayCmd_FailedConfigWriteRestoresSecret(t *testing.T) {
	const previous = "previous-token"
	secretsDirOf := func(e *gatewayEnv) string { return filepath.Join(e.xdg, "aide", "secrets") }

	t.Run("add --force keeps the overwritten token", func(t *testing.T) {
		e := newGatewayEnv(t)
		if _, err := runGateway(t, gwCmdToken, "add", "corp", "--url", e.srv.URL, "--token-stdin"); err != nil {
			t.Fatal(err)
		}
		dir := secretsDirOf(e)
		if err := secrets.NewManager(dir, "").SetKey("work", dir, "corp_token", previous, ""); err != nil {
			t.Fatal(err)
		}
		e.readOnlyConfig(t)
		other := strings.Replace(e.srv.URL, "127.0.0.1", "localhost", 1)
		if out, err := runGateway(t, gwCmdToken, "add", "corp", "--url", other, "--token-stdin", "--force"); err == nil {
			t.Fatalf("add --force succeeded with a read-only config:\n%s", out)
		}
		if got := e.store(t, "work"); got["corp_token"] != previous {
			t.Errorf("store after failed add --force = %v, want corp_token=%s", got, previous)
		}
	})

	t.Run("add of a new key leaves no store behind", func(t *testing.T) {
		e := newGatewayEnv(t)
		if err := os.MkdirAll(filepath.Join(e.xdg, "aide", "profiles"), 0o700); err != nil {
			t.Fatal(err)
		}
		e.readOnlyConfig(t)
		if out, err := runGateway(t, gwCmdToken, "add", "corp", "--url", e.srv.URL, "--token-stdin"); err == nil {
			t.Fatalf("add succeeded with a read-only config:\n%s", out)
		}
		if got := e.store(t, "work"); got != nil {
			t.Errorf("store after failed add = %v, want none", got)
		}
	})

	t.Run("edit keeps the rotated-out token", func(t *testing.T) {
		e := newGatewayEnv(t)
		if _, err := runGateway(t, gwCmdToken, "add", "corp", "--url", e.srv.URL, "--token-stdin"); err != nil {
			t.Fatal(err)
		}
		dir := secretsDirOf(e)
		if err := secrets.NewManager(dir, "").SetKey("work", dir, "corp_token", previous, ""); err != nil {
			t.Fatal(err)
		}
		e.readOnlyConfig(t)
		t.Setenv("NEW_CORP_TOKEN", gwCmdToken)
		if out, err := runGateway(t, "", "edit", "corp", "--token-env", "NEW_CORP_TOKEN", "--ttl", "6h", "--profile", "work"); err == nil {
			t.Fatalf("edit succeeded with a read-only config:\n%s", out)
		}
		if got := e.store(t, "work"); got["corp_token"] != previous {
			t.Errorf("store after failed edit = %v, want corp_token=%s", got, previous)
		}
	})
}

const remoteKeylessConfig = `profiles:
  work:
    agent: claude
    config_dir_name: work
    gateways:
      corp:
        base_url: https://gw.example.com
        harnesses: [claude]
default_profile: work
`

func TestGatewayCmd_KeylessRemoteRejected(t *testing.T) {
	t.Run("add", func(t *testing.T) {
		e := newGatewayEnv(t)
		before := e.config(t)
		out, err := runGateway(t, "", "add", "corp", "--url", "https://gw.example.com", "--profile", "work")
		// The probe or token requirement stops a keyless remote add first;
		// ValidateGatewayTarget is the backstop behind them.
		if err == nil {
			t.Fatalf("add succeeded\n%s", out)
		}
		if e.config(t) != before {
			t.Error("config changed")
		}
	})
	t.Run("edit moving off loopback", func(t *testing.T) {
		newGatewayEnv(t)
		srv := openAnonModels(t, `{"data":[{"id":"m1"}]}`)
		if out, err := runGateway(t, "", "add", "local", "--url", srv.URL, "--profile", "work"); err != nil {
			t.Fatalf("add: %v\n%s", err, out)
		}
		_, err := runGateway(t, "", "edit", "local", "--url", "https://gw.example.com", "--profile", "work")
		if err == nil || !strings.Contains(err.Error(), "keyless") {
			t.Fatalf("edit error = %v", err)
		}
	})
	t.Run("sync", func(t *testing.T) {
		e := newGatewayEnv(t)
		e.writeConfig(t, remoteKeylessConfig)
		out, err := runGateway(t, "", "sync", "--profile", "work")
		if err == nil || !strings.Contains(err.Error(), "could not sync gateway(s): corp") || !strings.Contains(out, "✗") {
			t.Fatalf("sync = %v\n%s", err, out)
		}
	})
	t.Run("remove still works", func(t *testing.T) {
		e := newGatewayEnv(t)
		e.writeConfig(t, remoteKeylessConfig)
		if out, err := runGateway(t, "", "remove", "corp", "--profile", "work"); err != nil {
			t.Fatalf("remove: %v\n%s", err, out)
		}
		if strings.Contains(e.config(t), "gw.example.com") {
			t.Error("gateway still in config")
		}
	})
	t.Run("edit to loopback succeeds", func(t *testing.T) {
		e := newGatewayEnv(t)
		e.writeConfig(t, remoteKeylessConfig)
		srv := openAnonModels(t, `{"data":[{"id":"m1"}]}`)
		if out, err := runGateway(t, "", "edit", "corp", "--url", srv.URL, "--profile", "work"); err != nil {
			t.Fatalf("edit: %v\n%s", err, out)
		}
	})
	t.Run("edit to keyed succeeds", func(t *testing.T) {
		e := newGatewayEnv(t)
		e.writeConfig(t, remoteKeylessConfig)
		out, err := runGateway(t, gwCmdToken+"\n", "edit", "corp", "--url", e.srv.URL, "--token-stdin", "--profile", "work")
		if err != nil {
			t.Fatalf("edit: %v\n%s", err, out)
		}
	})
}

func TestGatewayCmd_AddFromEnvAndDefaultFor(t *testing.T) {
	e := newGatewayEnv(t)
	t.Setenv("CORP_GATEWAY_KEY", gwCmdToken)
	out, err := runGateway(t, "", "add", "corp", "--url", e.srv.URL, "--from-env=CORP_GATEWAY_KEY",
		"--harnesses", "claude,pi", "--model", "m1", "--default-for", "pi", "--profile", "work")
	if err != nil {
		t.Fatalf("add --from-env: %v\n%s", err, out)
	}
	if got := e.store(t, "work"); got["corp_token"] != gwCmdToken {
		t.Errorf("stored key = %v", got)
	}
	gw := loadedGateway(t, e, "corp")
	if gw.Model != "m1" || gw.API != "anthropic-messages" {
		t.Errorf("gateway = %+v", gw)
	}
	loaded, err := config.Load(filepath.Join(e.xdg, "aide"), e.home)
	if err != nil {
		t.Fatal(err)
	}
	// pi was named; claude, a slot harness left unnamed, stays on its own login.
	if got := loaded.Contexts["work"].GatewayChoice; got["pi"] != "corp" || got["claude"] != "none" {
		t.Errorf("GatewayChoice = %v, want pi=corp claude=none", got)
	}
}

func TestGatewayCmd_AddFromEnvErrors(t *testing.T) {
	e := newGatewayEnv(t)
	t.Setenv("OPENROUTER_API_KEY", "")
	for name, tc := range map[string]struct {
		args []string
		want string
	}{
		"bare from-env on a custom gateway": {[]string{"add", "corp", "--url", e.srv.URL, "--from-env"}, "--from-env=VAR"},
		"preset key not exported":           {[]string{"add", "openrouter-anthropic", "--from-env"}, "OPENROUTER_API_KEY"},
		"no-auth with a key name":           {[]string{"add", "corp", "--url", e.srv.URL, "--no-auth", "--key", "k"}, "--no-auth takes no key"},
		"no-auth off loopback":              {[]string{"add", "corp", "--url", "https://llm.example.com", "--no-auth"}, "loopback"},
		"unknown wire protocol":             {[]string{"add", "corp", "--url", e.srv.URL, "--api", "grpc"}, "is not anthropic-messages"},
		"key for a provider switch":         {[]string{"add", "bedrock", "--region", "us-east-1", "--key-stdin"}, "takes no key"},
		"region on a custom gateway":        {[]string{"add", "corp", "--url", e.srv.URL, "--api", "openai-chat", "--region", "us-east-1"}, "only for the bedrock and vertex"},
		"project on bedrock":                {[]string{"add", "bedrock", "--region", "us-east-1", "--project", "p"}, "takes no --project"},
		"vertex without a project":          {[]string{"add", "vertex", "--region", "us-east5"}, "project: is required"},
		"key for ollama":                    {[]string{"add", "ollama", "--key-stdin"}, "takes no key"},
		"api contradicting a preset":        {[]string{"add", "openrouter", "--api", "anthropic-messages"}, "--api is for custom gateways"},
		"key and env together":              {[]string{"add", "corp", "--url", e.srv.URL, "--key-stdin", "--from-env=X"}, "mutually exclusive"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := runGateway(t, "", append(tc.args, "--profile", "work")...)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestGatewayCmd_AddDeprecatedAliasesStillWork(t *testing.T) {
	e := newGatewayEnv(t)
	t.Setenv("CORP_GATEWAY_KEY", gwCmdToken)
	if out, err := runGateway(t, "", "add", "corp", "--url", e.srv.URL, "--token-env", "CORP_GATEWAY_KEY", "--store", "team", "--ttl", "12h", "--profile", "work"); err != nil {
		t.Fatalf("add with deprecated flags: %v\n%s", err, out)
	}
	if gw := loadedGateway(t, e, "corp"); gw.Store != "team" || gw.RefreshTTL != "12h" {
		t.Errorf("gateway = %+v", gw)
	}
}

// routeToTestServer sends every default-transport request to srv, so a
// preset's remote URL can be probed in a test.
func routeToTestServer(t *testing.T, srv *httptest.Server) {
	t.Helper()
	orig := http.DefaultTransport
	u, _ := url.Parse(srv.URL)
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		r = r.Clone(r.Context())
		r.URL.Scheme, r.URL.Host = u.Scheme, u.Host
		return orig.RoundTrip(r)
	})
	t.Cleanup(func() { http.DefaultTransport = orig })
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestGatewayCmd_AddPresetFromEnvVar(t *testing.T) {
	e := newGatewayEnv(t)
	routeToTestServer(t, e.srv)
	t.Setenv("OPENROUTER_API_KEY", gwCmdToken)
	// openrouter and openrouter-anthropic share the variable; the first in
	// catalogue order is found.
	if out, err := runGateway(t, "", "add", "--from-env=OPENROUTER_API_KEY", "--profile", "work"); err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}
	gw := loadedGateway(t, e, "openrouter")
	if gw.Preset != "openrouter" || gw.API != "openai-chat" || gw.URL != "https://openrouter.ai/api/v1" ||
		strings.Join(gw.Harnesses, ",") != "pi,omp,opencode" {
		t.Errorf("gateway = %+v", gw)
	}
	if _, ok := loadedCtx(t, e).Gateways["openrouter-codex"]; ok {
		t.Error("unexpected openrouter-codex sibling")
	}
	if got := e.store(t, "work"); got["openrouter_token"] != gwCmdToken {
		t.Errorf("secret store = %v", got)
	}
}

func TestGatewayCmd_AddAnthropicPresetKeepsXAPIKey(t *testing.T) {
	e := newGatewayEnv(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != gwCmdToken {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"m1"}]}`))
	}))
	t.Cleanup(srv.Close)
	routeToTestServer(t, srv)
	if out, err := runGateway(t, gwCmdToken+"\n", "add", "anthropic", "--key-stdin", "--profile", "work"); err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}
	gw := loadedGateway(t, e, "anthropic")
	if gw.Preset != "anthropic" || gw.API != "anthropic-messages" || gw.URL != "https://api.anthropic.com" || gw.Store == "" || gw.Key == "" {
		t.Errorf("gateway = %+v", gw)
	}
}

func TestGatewayCmd_AddProviderSwitchSavesNoKey(t *testing.T) {
	e := newGatewayEnv(t)
	if out, err := runGateway(t, "", "add", "bedrock", "--region", "us-east-1", "--profile", "work"); err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}
	gw := loadedGateway(t, e, "bedrock")
	if gw.Preset != "bedrock" || gw.Region != "us-east-1" || gw.URL != "" || gw.BaseURL != "" || gw.Store != "" || gw.Key != "" {
		t.Errorf("gateway = %+v", gw)
	}
}

func TestGatewayCmd_AddGenerateAgeKeyOffTerminal(t *testing.T) {
	e := newGatewayEnv(t)
	t.Setenv("SOPS_AGE_KEY_FILE", "")
	t.Setenv("MY_GW_TOKEN", gwCmdToken)
	out, err := runGateway(t, "", "add", "corp", "--url", e.srv.URL, "--from-env=MY_GW_TOKEN", "--generate-age-key", "--profile", "work")
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}
	_, rest, ok := strings.Cut(out, "Generated age key at ")
	if !ok {
		t.Fatalf("no generated key reported:\n%s", out)
	}
	keyPath, _, _ := strings.Cut(rest, " (")
	t.Setenv("SOPS_AGE_KEY_FILE", keyPath)
	if got := e.store(t, "work"); got["corp_token"] != gwCmdToken {
		t.Errorf("secret store = %v", got)
	}
}

func TestGatewayCmd_AddNoAuthLoopbackIsKeyless(t *testing.T) {
	e := newGatewayEnv(t)
	srv := openAnonModels(t, `{"data":[{"id":"m1"}]}`)
	if out, err := runGateway(t, "", "add", "local", "--url", srv.URL, "--no-auth", "--profile", "work"); err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}
	if gw := loadedGateway(t, e, "local"); gw.Store != "" || gw.Key != "" || !gw.NoAuth {
		t.Errorf("gateway = %+v, want keyless", gw)
	}
}

func TestGatewayCmd_AddDefaultForUnservedHarness(t *testing.T) {
	e := newGatewayEnv(t)
	_, err := runGateway(t, gwCmdToken+"\n", "add", "corp", "--url", e.srv.URL, "--key-stdin", "--harnesses", "claude,pi", "--default-for", "opencode", "--profile", "work")
	if err == nil || !strings.Contains(err.Error(), "does not serve opencode") {
		t.Errorf("err = %v", err)
	}
	if strings.Contains(e.config(t), "gateways") {
		t.Error("a rejected add wrote config")
	}
}

func TestGatewayCmd_UseRepoSetsAndUnsets(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not installed")
	}
	e := newGatewayEnv(t)
	t.Setenv("PATH", filepath.Dir(git))
	for _, args := range [][]string{{"init", "-q", e.home}, {"-C", e.home, "remote", "add", "origin", "git@github.com:org/repo.git"}} {
		if out, err := exec.Command(git, args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if out, err := runGateway(t, gwCmdToken+"\n", "add", "corp", "--url", e.srv.URL, "--key-stdin", "--profile", "work"); err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}
	key := prefs.Key{Profile: "work", Remote: "github.com/org/repo"}
	got := func() string {
		f, err := prefs.Load(prefs.DefaultPath(e.home))
		if err != nil {
			t.Fatal(err)
		}
		p, _ := f.Get(key)
		return p.Gateway["pi"]
	}
	if out, err := runGateway(t, "", "use", "corp", "--harness", "pi", "--repo", "--profile", "work"); err != nil || got() != "corp" {
		t.Fatalf("use --repo: %v, pref = %q\n%s", err, got(), out)
	}
	if out, err := runGateway(t, "", "use", "--harness", "pi", "--repo", "--unset", "--profile", "work"); err != nil || got() != "" {
		t.Fatalf("use --repo --unset: %v, pref = %q\n%s", err, got(), out)
	}
}

func TestGatewayCmd_AddCustomOpenAIChat(t *testing.T) {
	e := newGatewayEnv(t)
	for _, api := range []string{"openai-chat", "openai-responses"} {
		name := "loc-" + api
		out, err := runGateway(t, gwCmdToken+"\n", "add", name, "--url", e.srv.URL+"/v1", "--api", api, "--key-stdin", "--profile", "work")
		if err != nil {
			t.Fatalf("add %s: %v\n%s", api, err, out)
		}
		wantH := "pi,omp,opencode"
		if api == "openai-responses" {
			wantH = "pi,omp,opencode,codex"
		}
		if !strings.Contains(out, "Harnesses: "+strings.ReplaceAll(wantH, ",", ", ")) {
			t.Errorf("%s: harness notice missing %q:\n%s", api, wantH, out)
		}
		gw := loadedGateway(t, e, name)
		if gw.URL != e.srv.URL+"/v1" || gw.API != api || strings.Join(gw.Harnesses, ",") != wantH {
			t.Errorf("%s: gateway = %+v", api, gw)
		}
		if _, ok := loadedCtx(t, e).Gateways[name+"-codex"]; ok {
			t.Errorf("%s: unexpected codex sibling", api)
		}
	}
}

func loadedCtx(t *testing.T, e *gatewayEnv) config.Context {
	t.Helper()
	loaded, err := config.Load(filepath.Join(e.xdg, "aide"), e.home)
	if err != nil {
		t.Fatal(err)
	}
	return loaded.Contexts["work"]
}

func TestGatewayCmd_AddKeylessCustomSavesNoAuth(t *testing.T) {
	e := newGatewayEnv(t)
	srv := openAnonModels(t, `{"data":[{"id":"m1"}]}`)
	if out, err := runGateway(t, "", "add", "loc", "--url", srv.URL+"/v1", "--api", "openai-chat", "--no-auth", "--profile", "work"); err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}
	if gw := loadedGateway(t, e, "loc"); !gw.NoAuth || gw.Key != "" {
		t.Errorf("gateway = %+v, want no_auth", gw)
	}
}

func TestGatewayCmd_AddOllamaPresetTakesNoKey(t *testing.T) {
	e := newGatewayEnv(t)
	routeToTestServer(t, openAnonModels(t, `{"data":[{"id":"m1"}]}`))
	if out, err := runGateway(t, "", "add", "ollama", "--profile", "work"); err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}
	gw := loadedGateway(t, e, "ollama")
	if gw.Preset != "ollama" || gw.Store != "" || gw.Key != "" || gw.API != "openai-chat" {
		t.Errorf("gateway = %+v", gw)
	}
	if got := e.store(t, "work"); got != nil {
		t.Errorf("secret store = %v, want none", got)
	}
}

func TestGatewayCmd_AddVertexSwitchAndSync(t *testing.T) {
	e := newGatewayEnv(t)
	out, err := runGateway(t, "", "add", "vertex", "--region", "us-east5", "--project", "p1", "--profile", "work")
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}
	if !strings.Contains(out, "opens the gcp capability") {
		t.Errorf("no capability warning:\n%s", out)
	}
	if gw := loadedGateway(t, e, "vertex"); gw.Region != "us-east5" || gw.Project != "p1" || gw.RefreshTTL != "" {
		t.Errorf("gateway = %+v", gw)
	}
	if out, err := runGateway(t, "", "sync", "vertex", "--profile", "work"); err != nil {
		t.Fatalf("sync: %v\n%s", err, out)
	}
	_ = filepath.WalkDir(e.home, func(p string, _ os.DirEntry, _ error) error {
		if strings.Contains(filepath.Base(p), "vertex") {
			t.Errorf("sync wrote %s", p)
		}
		return nil
	})
}
