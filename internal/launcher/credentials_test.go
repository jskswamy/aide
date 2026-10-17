package launcher

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"go.uber.org/mock/gomock"

	"github.com/jskswamy/aide/internal/config"
	aidectx "github.com/jskswamy/aide/internal/context"
	"github.com/jskswamy/aide/internal/diag"
	"github.com/jskswamy/aide/internal/launcher/mocks"
	"github.com/jskswamy/aide/internal/secrets"
)

func credentialProfile() config.Context {
	return config.Context{
		Agent: "claude",
		Credentials: map[string]config.CredentialBinding{
			"anthropic":  {Backend: "age", Store: "/stores/work", Key: "anthropic_api_key"},
			"openrouter": {Backend: "age", Store: "/stores/router", Key: "openrouter_api_key"},
			"corp":       {Backend: "age", Store: "/stores/work", Key: "corp", Env: []string{"CORP_KEY"}, Harnesses: []string{"pi"}},
		},
		DefaultCredentials: []string{"anthropic"},
	}
}

// fakeStores serves fixed store contents and records which were opened.
type fakeStores struct {
	data   map[string]map[string]string
	opened []string
}

func (f *fakeStores) load(path string) (map[string]string, error) {
	f.opened = append(f.opened, path)
	m, ok := f.data[path]
	if !ok {
		return nil, fmt.Errorf("no such store")
	}
	return m, nil
}

func newFakeStores() *fakeStores {
	return &fakeStores{data: map[string]map[string]string{
		"/stores/work":   {"anthropic_api_key": "sk-ant-work", "corp": "corp-secret"},
		"/stores/router": {"openrouter_api_key": "sk-or-router"},
	}}
}

func TestResolveCredentials_InjectsOnlySelectedBindings(t *testing.T) {
	plan, err := PlanCredentials("work", credentialProfile(), "claude", []string{"anthropic"}, "")
	if err != nil {
		t.Fatalf("PlanCredentials() error = %v", err)
	}
	if plan.Auth != AuthAPI {
		t.Errorf("Auth = %q, want api by default", plan.Auth)
	}
	stores := newFakeStores()
	env, err := ResolveCredentialEnv(plan, stores.load)
	if err != nil {
		t.Fatalf("ResolveCredentialEnv() error = %v", err)
	}
	want := map[string]string{"ANTHROPIC_API_KEY": "sk-ant-work"}
	if len(env) != 1 || env["ANTHROPIC_API_KEY"] != want["ANTHROPIC_API_KEY"] {
		t.Errorf("env = %v, want %v", env, want)
	}
	if !slices.Equal(stores.opened, []string{"/stores/work"}) {
		t.Errorf("opened stores %v, want only the selected binding's store", stores.opened)
	}
}

func TestResolveCredentials_DecryptsEachStoreOnce(t *testing.T) {
	ctx := credentialProfile()
	ctx.Agent = "pi"
	plan, err := PlanCredentials("work", ctx, "pi", []string{"anthropic", "corp", "anthropic"}, AuthAPI)
	if err != nil {
		t.Fatal(err)
	}
	stores := newFakeStores()
	env, err := ResolveCredentialEnv(plan, stores.load)
	if err != nil {
		t.Fatal(err)
	}
	if env["CORP_KEY"] != "corp-secret" || env["ANTHROPIC_API_KEY"] != "sk-ant-work" {
		t.Errorf("env = %v", env)
	}
	if len(stores.opened) != 1 {
		t.Errorf("opened %v, want the shared store once", stores.opened)
	}
}

func TestResolveCredentials_MissingKeyFails(t *testing.T) {
	ctx := credentialProfile()
	ctx.Credentials["anthropic"] = config.CredentialBinding{Backend: "age", Store: "/stores/work", Key: "nope"}
	plan, err := PlanCredentials("work", ctx, "claude", []string{"anthropic"}, AuthAPI)
	if err != nil {
		t.Fatal(err)
	}
	env, err := ResolveCredentialEnv(plan, newFakeStores().load)
	if !errors.Is(err, ErrCredentialKeyMissing) {
		t.Fatalf("error = %v, want ErrCredentialKeyMissing", err)
	}
	if env != nil {
		t.Errorf("env = %v, want nil on failure", env)
	}
	for _, w := range []string{"anthropic", "nope", "/stores/work"} {
		if !strings.Contains(err.Error(), w) {
			t.Errorf("error %q lacks %q", err, w)
		}
	}
	if strings.Contains(err.Error(), "sk-ant-work") || strings.Contains(err.Error(), "corp-secret") {
		t.Errorf("error leaks a value: %s", err)
	}
}

func TestResolveCredentials_MissingStoreFails(t *testing.T) {
	ctx := credentialProfile()
	ctx.Credentials["anthropic"] = config.CredentialBinding{Backend: "age", Store: "/stores/gone", Key: "k"}
	plan, err := PlanCredentials("work", ctx, "claude", []string{"anthropic"}, AuthAPI)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveCredentialEnv(plan, newFakeStores().load); err == nil || !strings.Contains(err.Error(), "/stores/gone") {
		t.Fatalf("error = %v, want one naming the store", err)
	}
}

func TestPlanCredentials_NoCrossProfileBorrowing(t *testing.T) {
	personal := config.Context{Agent: "claude"} // no credentials of its own
	_, err := PlanCredentials("personal", personal, "claude", []string{"anthropic"}, AuthAPI)
	if !errors.Is(err, ErrCredentialNotFound) {
		t.Fatalf("error = %v, want ErrCredentialNotFound", err)
	}
	if !strings.Contains(err.Error(), `"personal"`) || !strings.Contains(err.Error(), "anthropic") {
		t.Errorf("error %q should name the profile and credential", err)
	}
}

func TestPlanCredentials_NativeLoginInjectsNothing(t *testing.T) {
	plan, err := PlanCredentials("work", credentialProfile(), "claude", []string{"anthropic"}, AuthLogin)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Bindings) != 0 || !slices.Equal(plan.Ignored, []string{"anthropic"}) {
		t.Errorf("plan = %+v, want no bindings and anthropic ignored", plan)
	}
	stores := newFakeStores()
	env, err := ResolveCredentialEnv(plan, stores.load)
	if err != nil || len(env) != 0 || len(stores.opened) != 0 {
		t.Errorf("login resolved env=%v opened=%v err=%v, want nothing", env, stores.opened, err)
	}
}

func TestPlanCredentials_IncompatibleHarnessFails(t *testing.T) {
	_, err := PlanCredentials("work", credentialProfile(), "claude", []string{"corp"}, AuthAPI)
	if !errors.Is(err, ErrCredentialIncompatible) {
		t.Fatalf("error = %v, want ErrCredentialIncompatible", err)
	}
	if !strings.Contains(err.Error(), "pi") || !strings.Contains(err.Error(), "claude") {
		t.Errorf("error %q should name the allowed and actual harness", err)
	}
}

func TestPlanCredentials_EnvConflictFails(t *testing.T) {
	ctx := credentialProfile()
	ctx.Credentials["other"] = config.CredentialBinding{Backend: "age", Store: "/stores/work", Key: "k", Env: []string{"ANTHROPIC_API_KEY"}, Harnesses: []string{"claude"}}
	if _, err := PlanCredentials("work", ctx, "claude", []string{"anthropic", "other"}, AuthAPI); err == nil {
		t.Fatal("two bindings setting one env var should fail")
	}
}

func TestPlanCredentials_UnknownAuthModeFails(t *testing.T) {
	if _, err := PlanCredentials("work", credentialProfile(), "claude", nil, "token"); err == nil {
		t.Fatal("unknown auth mode should fail")
	}
}

// fixtureStore returns the encrypted test store and its decrypted
// values, with the age key exported for decryption.
func fixtureStore(t *testing.T) (string, map[string]string) {
	t.Helper()
	td := repoTestdataDir(t)
	keyFile := filepath.Join(td, "age-key.txt")
	encFile := filepath.Join(td, "test-secrets.enc.yaml")
	if _, err := os.Stat(encFile); err != nil {
		t.Skipf("test secrets fixture missing: %v", err)
	}
	t.Setenv("SOPS_AGE_KEY", "")
	t.Setenv("SOPS_AGE_KEY_FILE", keyFile)
	values, err := secrets.LoadSecretsMap(encFile)
	if err != nil {
		t.Skipf("cannot decrypt test secrets: %v", err)
	}
	return encFile, values
}

// launchCredentials launches configYAML and returns the agent env and
// stderr, or the launch error (the agent is never exec'd then).
func launchCredentials(t *testing.T, configYAML string, auth AuthMode, resolve bool) ([]string, string, error) {
	t.Helper()
	return launchCredentialsAs(t, configYAML, "", auth, resolve)
}

// launchCredentialsAs is launchCredentials with an --agent override.
func launchCredentialsAs(t *testing.T, configYAML, agentOverride string, auth AuthMode, resolve bool) ([]string, string, error) {
	t.Helper()
	return launchCredentialsIn(t, t.TempDir(), configYAML, agentOverride, auth, resolve)
}

// launchCredentialsIn is launchCredentialsAs with HOME set to home, so
// a test can seed native config dirs first.
func launchCredentialsIn(t *testing.T, home, configYAML, agentOverride string, auth AuthMode, resolve bool) ([]string, string, error) {
	t.Helper()
	t.Setenv("HOME", home)
	configDir := t.TempDir()
	cwd := t.TempDir()
	writeMinimalConfig(t, configDir, configYAML)

	ctrl := gomock.NewController(t)
	var capturedEnv []string
	mockExec := mocks.NewMockExecer(ctrl)
	mockExec.EXPECT().
		Exec(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ string, _ []string, env []string) error {
			capturedEnv = env
			return nil
		}).MaxTimes(1)
	var stderr bytes.Buffer
	l := &Launcher{
		Execer:    mockExec,
		ConfigDir: configDir,
		Stderr:    &stderr,
		Auth:      auth,
		LookPath:  func(file string) (string, error) { return "/usr/local/bin/" + file, nil },
	}
	err := l.Launch(cwd, agentOverride, nil, false, resolve, nil, nil)
	return capturedEnv, stderr.String(), err
}

func TestLauncher_InjectsDefaultCredentialsOnly(t *testing.T) {
	store, values := fixtureStore(t)
	t.Setenv("OPENAI_API_KEY", "sk-openai-shell")

	env, stderr, err := launchCredentials(t, fmt.Sprintf(`
profiles:
  work:
    agent: claude
    credentials:
      anthropic: {backend: age, store: %[1]s, key: anthropic_api_key}
      openai: {backend: age, store: %[1]s, key: openai_api_key}
    default_credentials: [anthropic]
default_profile: work
preferences:
  show_info: true
`, store), "", true)
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if got, _ := envValue(env, "ANTHROPIC_API_KEY"); got != values["anthropic_api_key"] {
		t.Errorf("ANTHROPIC_API_KEY not injected from the selected binding")
	}
	if v, ok := envValue(env, "OPENAI_API_KEY"); ok {
		t.Errorf("unselected OPENAI_API_KEY reached the agent (%d bytes)", len(v))
	}
	for k, v := range values {
		if strings.Contains(stderr, v) {
			t.Errorf("stderr (banner) contains the value of %s", k)
		}
	}
	if !strings.Contains(stderr, "anthropic") || !strings.Contains(stderr, "ANTHROPIC_API_KEY") {
		t.Errorf("banner should name the binding and its env var:\n%s", stderr)
	}
}

func TestLauncher_MissingCredentialKeyStopsBeforeExec(t *testing.T) {
	store, values := fixtureStore(t)
	_, stderr, err := launchCredentials(t, fmt.Sprintf(`
profiles:
  work:
    agent: claude
    credentials:
      anthropic: {backend: age, store: %s, key: no_such_key}
    default_credentials: [anthropic]
default_profile: work
`, store), "", false)
	if !errors.Is(err, ErrCredentialKeyMissing) {
		t.Fatalf("Launch() error = %v, want ErrCredentialKeyMissing", err)
	}
	for _, v := range values {
		if strings.Contains(err.Error(), v) || strings.Contains(stderr, v) {
			t.Error("a secret value was printed")
		}
	}
}

func TestLauncher_NativeLoginInjectsNoCredentials(t *testing.T) {
	store, _ := fixtureStore(t)
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-shell")
	env, _, err := launchCredentials(t, fmt.Sprintf(`
profiles:
  work:
    agent: claude
    credentials:
      anthropic: {backend: age, store: %s, key: anthropic_api_key}
    default_credentials: [anthropic]
default_profile: work
`, store), AuthLogin, false)
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if _, ok := envValue(env, "ANTHROPIC_API_KEY"); ok {
		t.Error("login launch injected ANTHROPIC_API_KEY")
	}
}

func TestLauncher_LegacySecretEnvBecomesBinding(t *testing.T) {
	store, values := fixtureStore(t)
	cfg := fmt.Sprintf(`
contexts:
  work:
    agent: claude
    secret: %s
    env:
      ANTHROPIC_API_KEY: "{{ .secrets.anthropic_api_key }}"
default_context: work
`, store)
	env, stderr, err := launchCredentials(t, cfg, "", false)
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if got, _ := envValue(env, "ANTHROPIC_API_KEY"); got != values["anthropic_api_key"] {
		t.Error("legacy ANTHROPIC_API_KEY was not injected")
	}
	if !strings.Contains(stderr, "ANTHROPIC_API_KEY") || !strings.Contains(stderr, "default_credentials") {
		t.Errorf("no migration hint:\n%s", stderr)
	}
	if strings.Contains(stderr, values["anthropic_api_key"]) {
		t.Error("hint printed the value")
	}
	// Native login drops the translated binding too.
	env, _, err = launchCredentials(t, cfg, AuthLogin, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := envValue(env, "ANTHROPIC_API_KEY"); ok {
		t.Error("login launch injected a legacy credential")
	}
}

func TestLauncher_LegacyLiteralCredentialStops(t *testing.T) {
	_, stderr, err := launchCredentials(t, `
contexts:
  work:
    agent: claude
    env:
      OPENAI_API_KEY: sk-literal-in-config
default_context: work
`, "", false)
	if !errors.Is(err, config.ErrLegacyCredentialEnv) {
		t.Fatalf("Launch() error = %v, want ErrLegacyCredentialEnv", err)
	}
	if !strings.Contains(err.Error(), "credentials:") || !strings.Contains(err.Error(), `"work"`) {
		t.Errorf("error lacks migration guidance: %v", err)
	}
	if strings.Contains(err.Error()+stderr, "sk-literal-in-config") {
		t.Error("the literal value was printed")
	}
}

func TestLauncher_ProjectConfigCannotSetProviderNames(t *testing.T) {
	for name, env := range map[string]string{
		"secret":   "OPENAI_API_KEY: sk-repo",
		"endpoint": "OPENAI_BASE_URL: https://repo.example",
		"selector": "CLAUDE_CODE_USE_BEDROCK: \"1\"",
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			configDir := t.TempDir()
			cwd := t.TempDir()
			writeMinimalConfig(t, configDir, `
contexts:
  work:
    agent: claude
default_context: work
`)
			if err := os.WriteFile(filepath.Join(cwd, ".aide.yaml"), []byte("env:\n  "+env+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			cfg, err := config.Load(configDir, cwd)
			if err != nil {
				t.Fatal(err)
			}
			rc, err := aidectx.Resolve(cfg, cwd, nil)
			if err != nil {
				t.Fatal(err)
			}
			_, _, err = profileCredentials(cfg, rc)
			if !errors.Is(err, config.ErrLegacyCredentialEnv) {
				t.Fatalf("profileCredentials() error = %v, want ErrLegacyCredentialEnv", err)
			}
			if !strings.Contains(err.Error(), "aide config") || strings.Contains(err.Error(), "repo.example") || strings.Contains(err.Error(), "sk-repo") {
				t.Errorf("error lacks guidance or echoes the value: %v", err)
			}
		})
	}
}

// A repository choosing a harness's config dir would choose which
// login the profile runs under, and could share one between profiles.
func TestLauncher_ProjectConfigCannotSetHarnessConfigDir(t *testing.T) {
	for _, key := range []string{"CLAUDE_CONFIG_DIR", "CODEX_HOME", "PI_CODING_AGENT_DIR"} {
		t.Run(key, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			configDir, cwd := t.TempDir(), t.TempDir()
			writeMinimalConfig(t, configDir, "profiles:\n  work:\n    agent: claude\ndefault_profile: work\n")
			if err := os.WriteFile(filepath.Join(cwd, ".aide.yaml"), []byte("env:\n  "+key+": ~/.shared\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			cfg, err := config.Load(configDir, cwd)
			if err != nil {
				t.Fatal(err)
			}
			rc, err := aidectx.Resolve(cfg, cwd, nil)
			if err != nil {
				t.Fatal(err)
			}
			_, _, err = profileCredentials(cfg, rc)
			if err == nil || !strings.Contains(err.Error(), key) || !strings.Contains(err.Error(), "aide config") {
				t.Fatalf("profileCredentials() error = %v, want a refusal naming %s with guidance", err, key)
			}
		})
	}
}

func TestLauncher_ScrubsCustomCredentialEnvFromShell(t *testing.T) {
	t.Setenv("CORP_LLM_TOKEN", "corp-shell")
	env, _, err := launchCredentials(t, `
profiles:
  work:
    agent: claude
    credentials:
      corp: {backend: age, store: work, key: corp, env: [CORP_LLM_TOKEN], harnesses: [pi]}
default_profile: work
`, "", false)
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if _, ok := envValue(env, "CORP_LLM_TOKEN"); ok {
		t.Error("a custom credential env name exported in the shell reached the agent unselected")
	}
}

func TestLauncher_LegacyHintPrintedOncePerLauncher(t *testing.T) {
	store, _ := fixtureStore(t)
	t.Setenv("HOME", t.TempDir())
	configDir := t.TempDir()
	cwd := t.TempDir()
	writeMinimalConfig(t, configDir, fmt.Sprintf(`
contexts:
  work:
    agent: claude
    secret: %s
    env:
      ANTHROPIC_API_KEY: "{{ .secrets.anthropic_api_key }}"
default_context: work
`, store))
	ctrl := gomock.NewController(t)
	mockExec := mocks.NewMockExecer(ctrl)
	mockExec.EXPECT().Exec(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(2)
	var stderr bytes.Buffer
	l := &Launcher{
		Execer:    mockExec,
		ConfigDir: configDir,
		Stderr:    &stderr,
		LookPath:  func(file string) (string, error) { return "/usr/local/bin/" + file, nil },
	}
	for range 2 {
		if err := l.Launch(cwd, "", nil, false, false, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	if n := strings.Count(stderr.String(), "default_credentials"); n != 1 {
		t.Errorf("hint printed %d times, want once:\n%s", n, stderr.String())
	}
}

// One profile serving two harnesses: default_credentials lists a
// binding per harness, and each launch injects only its own.
func TestLauncher_DefaultCredentialsSkipOtherHarnessBindings(t *testing.T) {
	store, values := fixtureStore(t)
	cfg := fmt.Sprintf(`
profiles:
  work:
    agent: claude
    credentials:
      openrouter: {backend: age, store: %[1]s, key: anthropic_api_key, harnesses: [pi]}
      anthropic: {backend: age, store: %[1]s, key: anthropic_api_key, harnesses: [claude]}
    default_credentials: [openrouter, anthropic]
default_profile: work
`, store)
	for _, tt := range []struct{ agent, want, notWant string }{
		{"claude", "ANTHROPIC_API_KEY", "OPENROUTER_API_KEY"},
		{"pi", "OPENROUTER_API_KEY", "ANTHROPIC_API_KEY"},
	} {
		t.Run(tt.agent, func(t *testing.T) {
			env, _, err := launchCredentialsAs(t, cfg, tt.agent, "", false)
			if err != nil {
				t.Fatalf("Launch(--agent %s) error = %v", tt.agent, err)
			}
			if got, _ := envValue(env, tt.want); got != values["anthropic_api_key"] {
				t.Errorf("%s not injected for %s", tt.want, tt.agent)
			}
			if _, ok := envValue(env, tt.notWant); ok {
				t.Errorf("%s injected for %s; it belongs to the other harness", tt.notWant, tt.agent)
			}
		})
	}
}

func TestPlanCredentials_ExplicitFlagIncompatibleStillFails(t *testing.T) {
	sel := Selection{}
	sel.SelectCredentials(SelectionInputs{Credentials: []string{"corp"}, CredentialsSet: true}, nil)
	if _, err := sel.PlanCredentials("work", credentialProfile(), "claude", AuthAPI); !errors.Is(err, ErrCredentialIncompatible) {
		t.Fatalf("--credential corp on claude: error = %v, want ErrCredentialIncompatible", err)
	}
	sel = Selection{}
	sel.SelectCredentials(SelectionInputs{}, []string{"corp"})
	plan, err := sel.PlanCredentials("work", credentialProfile(), "claude", AuthAPI)
	if err != nil || len(plan.Bindings) != 0 {
		t.Fatalf("default corp on claude: plan=%+v err=%v, want skipped", plan, err)
	}
	sel = Selection{}
	sel.SelectCredentials(SelectionInputs{}, []string{"missing"})
	if _, err := sel.PlanCredentials("work", credentialProfile(), "claude", AuthAPI); !errors.Is(err, ErrCredentialNotFound) {
		t.Fatalf("undefined default: error = %v, want ErrCredentialNotFound", err)
	}
}

func TestLauncher_AgentAliasMatchesBindingHarness(t *testing.T) {
	store, values := fixtureStore(t)
	env, _, err := launchCredentials(t, fmt.Sprintf(`
agents:
  my-claude:
    binary: claude
profiles:
  work:
    agent: my-claude
    credentials:
      anthropic: {backend: age, store: %s, key: anthropic_api_key, harnesses: [claude]}
    default_credentials: [anthropic]
default_profile: work
`, store), "", false)
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if got, _ := envValue(env, "ANTHROPIC_API_KEY"); got != values["anthropic_api_key"] {
		t.Error("binding for harness claude was not injected into the my-claude alias")
	}
}

func TestLauncher_ProviderSelectorInEnvLaunches(t *testing.T) {
	t.Setenv("CLAUDE_CODE_USE_BEDROCK", "shell")
	env, stderr, err := launchCredentials(t, `
profiles:
  work:
    agent: claude
    capabilities: [aws]
    env:
      CLAUDE_CODE_USE_BEDROCK: "1"
      ANTHROPIC_VERTEX_PROJECT_ID: proj
default_profile: work
`, "", false)
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if got, _ := envValue(env, "CLAUDE_CODE_USE_BEDROCK"); got != "1" {
		t.Errorf("CLAUDE_CODE_USE_BEDROCK = %q, want the profile's 1", got)
	}
	if got, _ := envValue(env, "ANTHROPIC_VERTEX_PROJECT_ID"); got != "proj" {
		t.Errorf("ANTHROPIC_VERTEX_PROJECT_ID = %q, want proj", got)
	}
	for _, line := range strings.Split(stderr, "\n") {
		if (strings.Contains(line, "warning") || strings.Contains(line, "note")) &&
			(strings.Contains(line, "CLAUDE_CODE_USE_BEDROCK") || strings.Contains(line, "ANTHROPIC_VERTEX_PROJECT_ID")) {
			t.Errorf("selector in env: was warned about: %s", line)
		}
	}
}

func TestLauncher_DiagnoseListsBindingStores(t *testing.T) {
	cfg := &config.Config{}
	rc := &aidectx.ResolvedContext{Name: "work", Context: config.Context{Secret: "work"}}
	plan := CredentialPlan{Auth: AuthAPI, Bindings: []PlannedCredential{
		{Name: "anthropic", Store: "work"}, {Name: "openrouter", Store: "router"},
	}}
	dc := (&Launcher{ConfigDir: t.TempDir()}).buildDiagContext(cfg, rc, plan, diag.SandboxInfo{})
	want := []string{config.ResolveSecretPath("work"), config.ResolveSecretPath("router")}
	if !slices.Equal(dc.SecretSourcePaths, want) {
		t.Errorf("SecretSourcePaths = %v, want %v", dc.SecretSourcePaths, want)
	}
}
