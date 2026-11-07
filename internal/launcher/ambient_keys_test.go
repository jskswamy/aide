package launcher

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"filippo.io/age"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/secrets"
	"github.com/jskswamy/aide/internal/trust"
)

func clearAmbientProviderVars(t *testing.T) {
	t.Helper()
	for _, entry := range os.Environ() {
		k, _, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		if strings.HasSuffix(k, "_API_KEY") || strings.HasSuffix(k, "_TOKEN") || strings.HasPrefix(k, "ANTHROPIC_") || strings.HasPrefix(k, "OPENAI_") || strings.HasPrefix(k, "GEMINI_") || strings.HasPrefix(k, "CODEX_") || strings.HasPrefix(k, "OPENROUTER_") {
			t.Setenv(k, "")
		}
	}
}

func TestDetectAmbientProviderKeys(t *testing.T) {
	environ := []string{
		"PATH=/usr/bin",
		"ANTHROPIC_API_KEY=sk-ant-1",
		"OPENAI_API_KEY=sk-openai-1",
		"GEMINI_API_KEY=",
		"GROQ_API_KEY=gsk-1",
		"AWS_BEARER_TOKEN_BEDROCK=aws-tok",
		"MALFORMED_ENTRY",
	}
	ctx := config.Context{
		Agent: "claude",
		Env: map[string]string{
			"GROQ_API_KEY": "already-in-env",
		},
		Credentials: map[string]config.CredentialBinding{
			"gemini": {Backend: "age", Store: "work", Key: "gemini_api_key"},
		},
		Gateways: map[string]config.GatewayConfig{
			"litellm": {BaseURL: "https://gw.example.com", Store: "work", Key: "litellm_key"},
		},
	}

	// Non-interactive (requireHarnessMatch = false): includes OPENAI_API_KEY and ANTHROPIC_API_KEY,
	// excludes GROQ_API_KEY (in ctx.Env) and AWS_BEARER_TOKEN_BEDROCK (in permitted).
	gotAll := detectAmbientProviderKeys(environ, ctx, []string{"AWS_BEARER_TOKEN_BEDROCK"}, "claude", false)
	wantAll := []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY"}
	if !slices.Equal(gotAll, wantAll) {
		t.Errorf("detectAmbientProviderKeys(requireHarnessMatch=false) = %v, want %v", gotAll, wantAll)
	}

	// Interactive (requireHarnessMatch = true) for claude: skips OPENAI_API_KEY because builtin "openai"
	// does not target claude by default.
	gotClaude := detectAmbientProviderKeys(environ, ctx, []string{"AWS_BEARER_TOKEN_BEDROCK"}, "claude", true)
	wantClaude := []string{"ANTHROPIC_API_KEY"}
	if !slices.Equal(gotClaude, wantClaude) {
		t.Errorf("detectAmbientProviderKeys(requireHarnessMatch=true, claude) = %v, want %v", gotClaude, wantClaude)
	}

	// If ANTHROPIC_API_KEY is already bound in ctx.Credentials, it is excluded.
	ctx.Credentials["anthropic"] = config.CredentialBinding{Backend: "age", Store: "work", Key: "anthropic_api_key"}
	if got := detectAmbientProviderKeys(environ, ctx, []string{"AWS_BEARER_TOKEN_BEDROCK"}, "claude", true); len(got) != 0 {
		t.Errorf("expected no ambient keys when anthropic is already bound, got %v", got)
	}
}

func TestAmbientKeys_NonInteractiveScrubNotice(t *testing.T) {
	clearAmbientProviderVars(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-super-secret-val")

	cwd := t.TempDir()
	env, stderr, err := selectionLaunch(t, selectionConfig, cwd, &Launcher{Interactive: false}, "")
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if _, ok := envValue(env, "ANTHROPIC_API_KEY"); ok {
		t.Error("expected ANTHROPIC_API_KEY to be scrubbed in non-interactive launch")
	}
	wantNote := `aide: note: scrubbed ambient ANTHROPIC_API_KEY from the environment (profile "work" has no credential binding; configure credentials: in your profile, run interactively to save, or pass --auth login to silence)`
	if !strings.Contains(stderr, wantNote) {
		t.Errorf("stderr missing scrub notice:\ngot:  %s\nwant: %s", stderr, wantNote)
	}
	if strings.Contains(stderr, "sk-ant-super-secret-val") {
		t.Error("stderr leaked the secret value")
	}

	// Suppressed when --auth login is set.
	_, stderrLogin, err := selectionLaunch(t, selectionConfig, cwd, &Launcher{Interactive: false, Auth: AuthLogin}, "")
	if err != nil {
		t.Fatalf("Launch(AuthLogin) error = %v", err)
	}
	if strings.Contains(stderrLogin, "scrubbed ambient") {
		t.Errorf("expected scrub notice to be suppressed under --auth login, got:\n%s", stderrLogin)
	}

	// Suppressed when --credential is explicitly set.
	_, stderrCredSet, err := selectionLaunch(t, selectionConfig, cwd, &Launcher{Interactive: false, CredentialsSet: true}, "")
	if err != nil {
		t.Fatalf("Launch(CredentialsSet) error = %v", err)
	}
	if strings.Contains(stderrCredSet, "scrubbed ambient") {
		t.Errorf("expected scrub notice to be suppressed when --credential is set, got:\n%s", stderrCredSet)
	}

	// Suppressed when a credential binding is active.
	store, _ := fixtureStore(t)
	boundCfg := `
profiles:
  work:
    agent: claude
    credentials:
      anthropic: {backend: age, store: ` + store + `, key: anthropic_api_key}
    default_credentials: [anthropic]
default_profile: work
`
	_, stderrBound, err := selectionLaunch(t, boundCfg, cwd, &Launcher{Interactive: false}, "")
	if err != nil {
		t.Fatalf("Launch(bound) error = %v", err)
	}
	if strings.Contains(stderrBound, "scrubbed ambient") {
		t.Errorf("expected scrub notice to be suppressed when binding is active, got:\n%s", stderrBound)
	}
}

func TestAmbientKeys_InteractiveOption1_AutoGeneratesAgeKeyAndBindsToProfile(t *testing.T) {
	clearAmbientProviderVars(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("SOPS_AGE_KEY", "")
	t.Setenv("SOPS_AGE_KEY_FILE", "")
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-onboard-opt1")

	cwd := t.TempDir()
	aideFile := filepath.Join(cwd, ".aide.yaml")
	writeGateFile(t, aideFile, "capabilities: [go]\nenv:\n  REPO_MARKER: keep-me\n")
	store := trust.NewStore(t.TempDir())
	trustFile(t, store, aideFile)

	// Select [1] Save to encrypted secret store & bind to profile "work",
	// then [1] (Enter) to generate a new age key.
	l := &Launcher{
		TrustStore:  store,
		Interactive: true,
		Stdin:       strings.NewReader("1\n\n"),
	}
	env, stderr, err := selectionLaunch(t, selectionConfig, cwd, l, "")
	if err != nil {
		t.Fatalf("Launch() error = %v\nstderr:\n%s", err, stderr)
	}
	if strings.Contains(stderr, "sk-ant-onboard-opt1") {
		t.Error("stderr leaked ambient secret value")
	}
	if !strings.Contains(stderr, "[aide] Ambient provider key(s) detected in shell environment: ANTHROPIC_API_KEY") {
		t.Errorf("stderr missing onboarding prompt:\n%s", stderr)
	}
	if got, _ := envValue(env, "ANTHROPIC_API_KEY"); got != "sk-ant-onboard-opt1" {
		t.Errorf("ANTHROPIC_API_KEY = %q, want sk-ant-onboard-opt1", got)
	}
	// Verify .aide.yaml overrides on rc.Context were preserved.
	if got, _ := envValue(env, "REPO_MARKER"); got != "keep-me" {
		t.Errorf("REPO_MARKER = %q, want keep-me (.aide.yaml env lost)", got)
	}
	if got, _ := envValue(env, "AIDE_CAPS"); !strings.Contains(got, "go") {
		t.Errorf("AIDE_CAPS = %q, want go (.aide.yaml capability lost)", got)
	}

	// Verify encrypted secret store exists under l.ConfigDir/secrets/work.enc.yaml.
	secretFile := filepath.Join(l.ConfigDir, "secrets", "work.enc.yaml")
	decrypted, err := secrets.LoadSecretsMap(secretFile)
	if err != nil {
		t.Fatalf("LoadSecretsMap(%s) error = %v", secretFile, err)
	}
	if decrypted["anthropic_api_key"] != "sk-ant-onboard-opt1" {
		t.Errorf("decrypted anthropic_api_key = %q, want sk-ant-onboard-opt1", decrypted["anthropic_api_key"])
	}

	// Verify profile config was persisted to disk.
	reloaded, err := config.Load(l.ConfigDir, cwd)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	workCtx := reloaded.Contexts["work"]
	if cb, ok := workCtx.Credentials["anthropic"]; !ok || cb.Store != "work" || cb.Key != "anthropic_api_key" {
		t.Errorf("reloaded work.Credentials[anthropic] = %+v (ok=%v)", cb, ok)
	}
	if !slices.Contains(workCtx.DefaultCredentials, "anthropic") {
		t.Errorf("reloaded work.DefaultCredentials = %v, want anthropic", workCtx.DefaultCredentials)
	}
}

func TestAmbientKeys_InteractiveOption1_ExternalAgePublicKeyFallback(t *testing.T) {
	clearAmbientProviderVars(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("SOPS_AGE_KEY", "")
	t.Setenv("SOPS_AGE_KEY_FILE", "")
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-external-key")

	extID, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}

	cwd := t.TempDir()
	// Select [1] Save to store, then [2] Enter an existing age public key, then the external public key.
	input := "1\n2\n" + extID.Recipient().String() + "\n"
	l := &Launcher{
		Interactive: true,
		Stdin:       strings.NewReader(input),
	}
	env, stderr, err := selectionLaunch(t, selectionConfig, cwd, l, "")
	if err != nil {
		t.Fatalf("Launch() error = %v\nstderr:\n%s", err, stderr)
	}
	if strings.Contains(stderr, "sk-ant-external-key") {
		t.Error("stderr leaked ambient secret value")
	}
	if got, _ := envValue(env, "ANTHROPIC_API_KEY"); got != "sk-ant-external-key" {
		t.Errorf("ANTHROPIC_API_KEY = %q, want sk-ant-external-key from fallback", got)
	}
	secretFile := filepath.Join(l.ConfigDir, "secrets", "work.enc.yaml")
	if _, err := os.Stat(secretFile); err != nil {
		t.Errorf("expected encrypted store at %s: %v", secretFile, err)
	}
}

func TestAmbientKeys_InteractiveCrossHarnessFiltering(t *testing.T) {
	clearAmbientProviderVars(t)
	t.Setenv("HOME", t.TempDir())
	// OPENAI_API_KEY is set, but active harness is claude.
	t.Setenv("OPENAI_API_KEY", "sk-openai-cross-harness")

	cwd := t.TempDir()
	l := &Launcher{
		Interactive: true,
		Stdin:       failingReader{},
	}
	env, stderr, err := selectionLaunch(t, selectionConfig, cwd, l, "")
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if strings.Contains(stderr, "Ambient provider key(s) detected") {
		t.Errorf("cross-harness OPENAI_API_KEY should not trigger interactive prompt on claude:\n%s", stderr)
	}
	if _, ok := envValue(env, "OPENAI_API_KEY"); ok {
		t.Error("OPENAI_API_KEY should still be scrubbed from claude's environment")
	}
}

func TestAmbientKeys_InteractiveOption2_CreateNewProfileForFolder(t *testing.T) {
	clearAmbientProviderVars(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	keyFile := filepath.Join(home, "keys.txt")
	if _, err := secrets.GenerateDefaultAgeKey(keyFile); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SOPS_AGE_KEY", "")
	t.Setenv("SOPS_AGE_KEY_FILE", keyFile)
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-opt2-folder")

	parentDir := t.TempDir()
	cwd := filepath.Join(parentDir, "myproj")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}

	// Select [2] Create a new profile for this folder, then press Enter to accept default name "myproj".
	l := &Launcher{
		Interactive: true,
		Stdin:       strings.NewReader("2\n\n"),
	}
	env, stderr, err := selectionLaunch(t, selectionConfig, cwd, l, "")
	if err != nil {
		t.Fatalf("Launch() error = %v\nstderr:\n%s", err, stderr)
	}
	if strings.Contains(stderr, "sk-ant-opt2-folder") {
		t.Error("stderr leaked ambient secret value")
	}
	if got, _ := envValue(env, "AIDE_PROFILE"); got != "myproj" {
		t.Errorf("AIDE_PROFILE = %q, want myproj", got)
	}
	if got, _ := envValue(env, "ANTHROPIC_API_KEY"); got != "sk-ant-opt2-folder" {
		t.Errorf("ANTHROPIC_API_KEY = %q, want sk-ant-opt2-folder", got)
	}
	if got, _ := envValue(env, "CLAUDE_CONFIG_DIR"); got != filepath.Join(home, ".claude-myproj") {
		t.Errorf("CLAUDE_CONFIG_DIR = %q, want %s", got, filepath.Join(home, ".claude-myproj"))
	}

	// Verify secret store and new profile persisted.
	secretFile := filepath.Join(l.ConfigDir, "secrets", "myproj.enc.yaml")
	if _, err := os.Stat(secretFile); err != nil {
		t.Errorf("expected secret store at %s: %v", secretFile, err)
	}
	reloaded, err := config.Load(l.ConfigDir, cwd)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	newCtx, ok := reloaded.Contexts["myproj"]
	if !ok {
		t.Fatalf("reloaded config missing myproj profile")
	}
	if len(newCtx.Match) != 1 || newCtx.Match[0].Path != cwd {
		t.Errorf("myproj.Match = %+v, want path %s", newCtx.Match, cwd)
	}

	// Duplicate profile name returns an error.
	lDup := &Launcher{
		Interactive: true,
		Stdin:       strings.NewReader("2\nwork\n"),
	}
	if _, _, err := selectionLaunch(t, selectionConfig, cwd, lDup, ""); err == nil || !strings.Contains(err.Error(), `profile "work" already exists`) {
		t.Errorf("expected duplicate profile error, got: %v", err)
	}

	// Git remote match rule when cwd has an origin remote, and custom profile name.
	gitDir := t.TempDir()
	gitRepo(t, gitDir, "git@github.com:org/ambient-repo.git")
	lGit := &Launcher{
		Interactive: true,
		Stdin:       strings.NewReader("2\nrepo-prof\n"),
	}
	envGit, _, err := selectionLaunch(t, selectionConfig, gitDir, lGit, "")
	if err != nil {
		t.Fatalf("Launch(git) error = %v", err)
	}
	if got, _ := envValue(envGit, "AIDE_PROFILE"); got != "repo-prof" {
		t.Errorf("AIDE_PROFILE = %q, want repo-prof", got)
	}
	reloadedGit, err := config.Load(lGit.ConfigDir, gitDir)
	if err != nil {
		t.Fatalf("config.Load(git) error = %v", err)
	}
	if gotMatch := reloadedGit.Contexts["repo-prof"].Match; len(gotMatch) != 1 || gotMatch[0].Remote != "github.com/org/ambient-repo" {
		t.Errorf("repo-prof.Match = %+v, want remote github.com/org/ambient-repo", gotMatch)
	}

	// Invalid menu selection returns an error.
	lBad := &Launcher{
		Interactive: true,
		Stdin:       strings.NewReader("9\n"),
	}
	if _, _, err := selectionLaunch(t, selectionConfig, cwd, lBad, ""); err == nil || !strings.Contains(err.Error(), "invalid selection") {
		t.Errorf("expected invalid selection error, got: %v", err)
	}
}

func TestAmbientKeys_InteractiveOption3_OneTimePassThrough(t *testing.T) {
	clearAmbientProviderVars(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-opt3-once")

	cwd := t.TempDir()
	l := &Launcher{
		Interactive: true,
		Stdin:       strings.NewReader("3\n"),
	}
	env, stderr, err := selectionLaunch(t, selectionConfig, cwd, l, "")
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if got, _ := envValue(env, "ANTHROPIC_API_KEY"); got != "sk-ant-opt3-once" {
		t.Errorf("ANTHROPIC_API_KEY = %q, want sk-ant-opt3-once", got)
	}
	if !strings.Contains(stderr, "aide: note: passing ambient ANTHROPIC_API_KEY for this launch only") {
		t.Errorf("stderr missing one-time note:\n%s", stderr)
	}
	if strings.Contains(stderr, "sk-ant-opt3-once") {
		t.Error("stderr leaked ambient secret value")
	}
	if _, err := os.Stat(filepath.Join(l.ConfigDir, "secrets")); !os.IsNotExist(err) {
		t.Errorf("option 3 should not create secrets dir, stat err = %v", err)
	}
}

func TestAmbientKeys_InteractiveOption4_StripAndContinue(t *testing.T) {
	for _, input := range []string{"4\n", "\n", ""} {
		t.Run("input_"+strings.TrimSpace(input), func(t *testing.T) {
			clearAmbientProviderVars(t)
			t.Setenv("HOME", t.TempDir())
			t.Setenv("ANTHROPIC_API_KEY", "sk-ant-opt4-strip")

			cwd := t.TempDir()
			l := &Launcher{
				Interactive: true,
				Stdin:       strings.NewReader(input),
			}
			env, stderr, err := selectionLaunch(t, selectionConfig, cwd, l, "")
			if err != nil {
				t.Fatalf("Launch() error = %v", err)
			}
			if _, ok := envValue(env, "ANTHROPIC_API_KEY"); ok {
				t.Error("expected ANTHROPIC_API_KEY to be stripped on option 4")
			}
			if strings.Contains(stderr, "sk-ant-opt4-strip") {
				t.Error("stderr leaked ambient secret value")
			}
		})
	}
}

func TestAmbientKeys_InteractiveOption1_CustomProviderKey(t *testing.T) {
	clearAmbientProviderVars(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	keyFile := filepath.Join(home, "keys.txt")
	if _, err := secrets.GenerateDefaultAgeKey(keyFile); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SOPS_AGE_KEY", "")
	t.Setenv("SOPS_AGE_KEY_FILE", keyFile)
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "tok-custom-provider")

	cwd := t.TempDir()
	l := &Launcher{
		Interactive: true,
		Stdin:       strings.NewReader("1\n"),
	}
	env, stderr, err := selectionLaunch(t, selectionConfig, cwd, l, "")
	if err != nil {
		t.Fatalf("Launch() error = %v\nstderr:\n%s", err, stderr)
	}
	if got, _ := envValue(env, "ANTHROPIC_AUTH_TOKEN"); got != "tok-custom-provider" {
		t.Errorf("ANTHROPIC_AUTH_TOKEN = %q, want tok-custom-provider", got)
	}
}
