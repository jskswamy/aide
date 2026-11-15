package launcher

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/config"
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
	wantNote := `aide: note: scrubbed ambient ANTHROPIC_API_KEY from the environment (profile "work" has no credential binding); configure a gateway with aide gateway add <name> --url ... or pass --auth login to silence`
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

// An interactive launch is not interrupted either: the key is still
// scrubbed.
func TestAmbientKeys_InteractiveLaunchNeverPrompts(t *testing.T) {
	clearAmbientProviderVars(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("OPENAI_API_KEY", "sk-openai-hint")

	l := &Launcher{Interactive: true, Stdin: failingReader{}}
	env, stderr, err := selectionLaunch(t, selectionConfig, t.TempDir(), l, "")
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if _, ok := envValue(env, "OPENAI_API_KEY"); ok {
		t.Error("OPENAI_API_KEY should be scrubbed")
	}
	// OpenAI keys are not for the claude harness this profile launches.
	if strings.Contains(stderr, "scrubbed ambient") || strings.Contains(stderr, "--from-env") {
		t.Errorf("stderr notes an irrelevant key or a flag that does not exist:\n%s", stderr)
	}
	if strings.Contains(stderr, "sk-openai-hint") || strings.Contains(stderr, "Select [") {
		t.Errorf("stderr leaked the value or prompted:\n%s", stderr)
	}
}
