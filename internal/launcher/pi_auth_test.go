package launcher

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writePiAuth(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "auth.json"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

const piAuthWithLogins = `{
  "anthropic": {"type": "oauth", "refresh": "rt-secret", "access": "at-secret", "expires": 1},
  "openrouter": {"type": "api_key", "key": "sk-or-secret"}
}`

func TestPiStoredLoginNotice(t *testing.T) {
	dir := t.TempDir()
	writePiAuth(t, dir, piAuthWithLogins)

	for name, tc := range map[string]struct {
		env  []string
		want []string // provider names the notice must list; nil = no notice
	}{
		"selected key shadowed":         {[]string{"ANTHROPIC_API_KEY"}, []string{"anthropic"}},
		"two providers shadowed":        {[]string{"OPENROUTER_API_KEY", "ANTHROPIC_API_KEY"}, []string{"anthropic", "openrouter"}},
		"no stored login for provider":  {[]string{"OPENAI_API_KEY"}, nil},
		"custom name with no provider":  {[]string{"MY_PROXY_KEY"}, nil},
		"nothing selected (login mode)": {nil, nil},
	} {
		t.Run(name, func(t *testing.T) {
			got := piStoredLoginNotice(dir, tc.env)
			if tc.want == nil {
				if got != "" {
					t.Errorf("notice = %q, want none", got)
				}
				return
			}
			if !strings.Contains(got, strings.Join(tc.want, ", ")) {
				t.Errorf("notice = %q, want providers %v", got, tc.want)
			}
			for _, secret := range []string{"rt-secret", "at-secret", "sk-or-secret", "oauth", "api_key"} {
				if strings.Contains(got, secret) {
					t.Errorf("notice reveals stored credential content %q: %q", secret, got)
				}
			}
		})
	}
}

func TestPiStoredLoginNotice_MissingOrInvalidAuthFile(t *testing.T) {
	if got := piStoredLoginNotice(t.TempDir(), []string{"ANTHROPIC_API_KEY"}); got != "" {
		t.Errorf("missing auth.json: notice = %q, want none", got)
	}
	dir := t.TempDir()
	writePiAuth(t, dir, "{not json")
	if got := piStoredLoginNotice(dir, []string{"ANTHROPIC_API_KEY"}); got != "" {
		t.Errorf("invalid auth.json: notice = %q, want none", got)
	}
}

// A Pi profile launch that injects a selected key for a provider with a
// stored login in the profile's auth.json says the login wins in Pi.
func TestLauncher_PiStoredLoginShadowsSelectedKey(t *testing.T) {
	store, values := fixtureStore(t)
	home := t.TempDir()
	writePiAuth(t, filepath.Join(home, ".pi-work"), piAuthWithLogins)

	cfg := fmt.Sprintf(`
profiles:
  work:
    agent: pi
    config_dir_name: work
    credentials:
      anthropic: {backend: age, store: %s, key: anthropic_api_key}
    default_credentials: [anthropic]
default_profile: work
`, store)
	_, stderr, err := launchCredentialsIn(t, home, cfg, "", "", true)
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if !strings.Contains(stderr, "stored Pi login for anthropic") {
		t.Errorf("no stored-login notice:\n%s", stderr)
	}
	for _, v := range values {
		if strings.Contains(stderr, v) {
			t.Error("a secret value was printed")
		}
	}

	// --auth login injects no key, so nothing is shadowed.
	_, stderr, err = launchCredentialsIn(t, home, cfg, "", AuthLogin, true)
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if strings.Contains(stderr, "stored Pi login") {
		t.Errorf("login launch printed the stored-login notice:\n%s", stderr)
	}
}

// A Pi profile without a config dir of its own uses the default
// ~/.pi/agent (or an exported PI_CODING_AGENT_DIR); its stored logins
// shadow a selected key just the same.
func TestLauncher_PiStoredLoginNoticeWithoutProfileDir(t *testing.T) {
	store, _ := fixtureStore(t)
	cfg := fmt.Sprintf(`
profiles:
  work:
    agent: pi
    credentials:
      anthropic: {backend: age, store: %s, key: anthropic_api_key}
    default_credentials: [anthropic]
default_profile: work
`, store)

	home := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", "")
	if err := os.Unsetenv("PI_CODING_AGENT_DIR"); err != nil {
		t.Fatal(err)
	}
	writePiAuth(t, filepath.Join(home, ".pi", "agent"), piAuthWithLogins)
	_, stderr, err := launchCredentialsIn(t, home, cfg, "", "", true)
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if !strings.Contains(stderr, filepath.Join(home, ".pi", "agent", "auth.json")+" has a stored Pi login for anthropic") {
		t.Errorf("no notice for the default Pi dir:\n%s", stderr)
	}

	home = t.TempDir()
	exported := filepath.Join(home, "my-pi")
	writePiAuth(t, exported, piAuthWithLogins)
	t.Setenv("PI_CODING_AGENT_DIR", exported)
	_, stderr, err = launchCredentialsIn(t, home, cfg, "", "", true)
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if !strings.Contains(stderr, filepath.Join(exported, "auth.json")+" has a stored Pi login") {
		t.Errorf("no notice for an exported PI_CODING_AGENT_DIR:\n%s", stderr)
	}
}
