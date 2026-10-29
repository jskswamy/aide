package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/gateway"
	"github.com/jskswamy/aide/internal/output"
	"github.com/jskswamy/aide/internal/prefs"
	"github.com/jskswamy/aide/internal/secrets"
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
	if err := os.WriteFile(filepath.Join(e.xdg, "aide", "config.yaml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func (e *gatewayEnv) config(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(e.xdg, "aide", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
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
	if gw.BaseURL != want.BaseURL || gw.RefreshTTL != want.RefreshTTL || gw.Store != want.Store || gw.Key != want.Key || gw.Harnesses != nil {
		t.Errorf("gateways.corp = %+v, want %+v", gw, want)
	}
	if got := e.store(t, "work"); got["corp_token"] != gwCmdToken {
		t.Errorf("secret store = %v", got)
	}
	if cat, err := gateway.ReadCache(e.home, "work", "corp"); err != nil || cat == nil || len(cat.Models) != 2 {
		t.Errorf("cache = %+v, %v", cat, err)
	}
	data, err := os.ReadFile(filepath.Join(e.home, ".pi-work", "models.json"))
	if err != nil || strings.Contains(string(data), gwCmdToken) || !strings.Contains(string(data), "CORP_TOKEN") {
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
	if gw.RefreshTTL != "12h" || gw.Key != "llm_key" || gw.Store != "gw" || strings.Join(gw.Harnesses, ",") != "pi,codex" {
		t.Errorf("gateway = %+v", gw)
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
		"missing url non-tty":      {[]string{"add", "corp", "--token-env", "MY_GW_TOKEN"}, "--url is required"},
		"missing url token-stdin":  {[]string{"add", "corp", "--token-stdin"}, "--url is required"},
		"both token sources":       {[]string{"add", "corp", "--url", e.srv.URL, "--token-stdin", "--token-env", "MY_GW_TOKEN"}, "mutually exclusive"},
		"empty token env":          {[]string{"add", "corp", "--url", e.srv.URL, "--token-env", "UNSET_GW_TOKEN"}, "UNSET_GW_TOKEN"},
		"no token source":          {[]string{"add", "corp", "--url", e.srv.URL}, "--token-stdin"},
		"remote http":              {[]string{"add", "corp", "--url", "http://gw.example.com", "--token-env", "MY_GW_TOKEN"}, "https"},
		"unknown profile":          {[]string{"add", "corp", "--url", e.srv.URL, "--token-env", "MY_GW_TOKEN", "--profile", "nope"}, "nope"},
		"bad harness":              {[]string{"add", "corp", "--url", e.srv.URL, "--token-env", "MY_GW_TOKEN", "--harnesses", "gemini"}, "gemini"},
		"dangerous key":            {[]string{"add", "corp", "--url", e.srv.URL, "--token-env", "MY_GW_TOKEN", "--key", "ld_preload"}, "LD_PRELOAD"},
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

func TestGatewayCmd_List(t *testing.T) {
	e := newGatewayEnv(t)
	if _, err := runGateway(t, gwCmdToken, "add", "corp", "--url", e.srv.URL, "--token-stdin"); err != nil {
		t.Fatal(err)
	}
	out, err := runGateway(t, "", "list", "--profile", "work", "--models")
	if err != nil {
		t.Fatalf("list: %v\n%s", err, out)
	}
	for _, want := range []string{
		"PROFILE: work", "GATEWAY: corp", e.srv.URL, "store=work, key=corp_token", "2 models",
		"ANTHROPIC_AUTH_TOKEN", "CORP_TOKEN", "pending", "Model One", "acme",
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
	if len(entries) != 1 || entries[0].Profile != "work" || entries[0].Gateway != "corp" ||
		entries[0].ModelCount != 2 || len(entries[0].Models) != 2 || len(entries[0].Harnesses) != 4 || entries[0].Expired {
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
	for _, want := range []string{`Removed gateway "corp"`, `Deleted secret key "corp_token"`, "Removed cache", `removed provider "corp"`} {
		if !strings.Contains(out, want) {
			t.Errorf("remove output missing %q:\n%s", want, out)
		}
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
	cfg := strings.Replace(e.config(t), "        config_dir_name: home\n",
		"        config_dir_name: home\n        secret: work\n        env:\n            CORP: '{{ .secrets.corp_token }}'\n", 1)
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
	e.writeConfig(t, `profiles:
  work:
    agent: claude
    config_dir_name: work
    credentials:
      anthropic:
        backend: age
        store: work
        key: anthropic_api_key
    default_credentials: [anthropic]
default_profile: work
`)
	if _, err := runGateway(t, gwCmdToken, "add", "api", "--url", e.srv.URL, "--token-stdin", "--profile", "work"); err != nil {
		t.Fatal(err)
	}

	// List shows both built-in anthropic (default for claude, pi, opencode) and api (default for codex).
	out, err := runGateway(t, "", "list", "--profile", "work")
	if err != nil {
		t.Fatalf("list: %v\n%s", err, out)
	}
	for _, want := range []string{
		"GATEWAY: anthropic (default for: claude, pi, opencode",
		"https://api.anthropic.com (built-in)",
		"GATEWAY: api (default for: codex",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("list missing %q:\n%s", want, out)
		}
	}

	// Switch default to api via `aide gateway use api --profile work`.
	out, err = runGateway(t, "", "use", "api", "--profile", "work")
	if err != nil {
		t.Fatalf("use api: %v\n%s", err, out)
	}
	if !strings.Contains(out, `Set default gateway for profile "work" to "api" (claude, pi, opencode, codex)`) {
		t.Errorf("unexpected use output:\n%s", out)
	}
	loaded, err := config.Load(filepath.Join(e.xdg, "aide"), e.home)
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.Contexts["work"].DefaultCredentials; len(got) != 1 || got[0] != "api" {
		t.Errorf("DefaultCredentials after use api = %v, want [api]", got)
	}

	// Switch back to anthropic via interactive TTY picker.
	orig := stdinIsTTY
	stdinIsTTY = func() bool { return true }
	t.Cleanup(func() { stdinIsTTY = orig })
	out, err = runGateway(t, "1\n", "use", "--profile", "work")
	if err != nil {
		t.Fatalf("interactive use: %v\n%s", err, out)
	}
	if !strings.Contains(out, `Set default gateway for profile "work" to "anthropic"`) {
		t.Errorf("unexpected interactive use output:\n%s", out)
	}
}

func TestGatewayCmd_AddInteractiveNameAndDefaultPrompt(t *testing.T) {
	e := newGatewayEnv(t)
	e.writeConfig(t, `profiles:
  work:
    agent: claude
    config_dir_name: work
    credentials:
      anthropic:
        backend: age
        store: work
        key: anthropic_api_key
    default_credentials: [anthropic]
default_profile: work
`)
	orig := stdinIsTTY
	stdinIsTTY = func() bool { return true }
	t.Cleanup(func() { stdinIsTTY = orig })

	url := strings.Replace(e.srv.URL, "127.0.0.1", "localhost", 1)
	stdin := strings.Join([]string{url, "acme", gwCmdToken, "y"}, "\n") + "\n"
	out, err := runGateway(t, stdin, "add", "--profile", "work")
	if err != nil {
		t.Fatalf("interactive add: %v\n%s", err, out)
	}
	for _, want := range []string{
		"Gateway base URL",
		"Gateway name [localhost]: ",
		`Make "acme" the default instead? [Y/n]: `,
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
	if got := loaded.Contexts["work"].DefaultCredentials; len(got) != 1 || got[0] != "acme" {
		t.Errorf("DefaultCredentials = %v, want [acme]", got)
	}
}

func TestGatewayCmd_AddPromptsForNameWhenURLFlagPassedOnTTY(t *testing.T) {
	e := newGatewayEnv(t)
	orig := stdinIsTTY
	stdinIsTTY = func() bool { return true }
	t.Cleanup(func() { stdinIsTTY = orig })

	url := strings.Replace(e.srv.URL, "127.0.0.1", "localhost", 1)
	stdin := strings.Join([]string{"acme", gwCmdToken}, "\n") + "\n"
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
	if _, err := runGateway(t, gwCmdToken, "add", "api", "--url", e.srv.URL, "--default", "--token-stdin", "--profile", "work"); err != nil {
		t.Fatal(err)
	}

	// Seed a repository preference referencing "api".
	prefPath := prefs.DefaultPath(e.home)
	pf := &prefs.File{Version: prefs.Version}
	pf.Set(prefs.Key{Profile: "work", Remote: "github.com/org/repo"}, prefs.Preference{
		Credentials: prefs.Selection("api"),
	})
	if err := prefs.Save(prefPath, pf); err != nil {
		t.Fatal(err)
	}

	out, err := runGateway(t, "", "rename", "api", "acme", "--profile", "work")
	if err != nil {
		t.Fatalf("rename api acme: %v\n%s", err, out)
	}
	for _, want := range []string{
		`Migrated secret key "api_token" → "acme_token"`,
		`Renamed gateway "api" → "acme" under profile "work"`,
		`Moved cache`,
		`env: ACME_TOKEN`,
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
	if len(wctx.DefaultCredentials) != 1 || wctx.DefaultCredentials[0] != "acme" {
		t.Errorf("DefaultCredentials = %v, want [acme]", wctx.DefaultCredentials)
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
	if strings.Contains(string(piRaw), `"$API_TOKEN"`) || !strings.Contains(string(piRaw), `"acme"`) || !strings.Contains(string(piRaw), `"$ACME_TOKEN"`) {
		t.Errorf("pi models.json after rename:\n%s", piRaw)
	}
	codexRaw, err := os.ReadFile(filepath.Join(e.home, ".codex-work", "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(codexRaw), "[model_providers.api]") || strings.Contains(string(codexRaw), "API_TOKEN") || !strings.Contains(string(codexRaw), "model_provider = 'acme'") {
		t.Errorf("codex config.toml after rename:\n%s", codexRaw)
	}

	// Verify repository preference updated to "acme".
	updatedPrefs, err := prefs.Load(prefPath)
	if err != nil {
		t.Fatal(err)
	}
	if p, ok := updatedPrefs.Get(prefs.Key{Profile: "work", Remote: "github.com/org/repo"}); !ok || p.Credentials == nil || (*p.Credentials)[0] != "acme" {
		t.Errorf("repo preference after rename = %+v, ok=%v", p, ok)
	}
}

func TestGatewayCmd_EditInteractiveAndTokenRotation(t *testing.T) {
	var currentToken atomic.Value
	currentToken.Store(gwCmdToken)

	e := newGatewayEnv(t)
	rotSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want := "Bearer " + currentToken.Load().(string)
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
	if _, err := runGateway(t, gwCmdToken, "add", "corp", "--url", rotSrv.URL, "--token-stdin", "--profile", "work"); err != nil {
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
