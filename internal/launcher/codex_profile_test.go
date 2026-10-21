package launcher

import (
	"bytes"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"go.uber.org/mock/gomock"

	"github.com/jskswamy/aide/internal/launcher/mocks"
)

func TestLauncher_CodexProfileContract(t *testing.T) {
	for _, tc := range []struct {
		name       string
		profile    string
		auth       AuthMode
		explicit   bool
		wantNotice bool
	}{
		{name: "work falls back inside its own home", profile: "work", wantNotice: true},
		{name: "personal uses its own home", profile: "personal"},
		{name: "explicit login ignores default credentials", profile: "work", auth: AuthLogin},
		{name: "explicit incompatible credential fails", profile: "work", explicit: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("CODEX_HOME", filepath.Join(home, ".codex-other"))
			keys := []string{"CODEX_API_KEY", "CODEX_ACCESS_TOKEN", "OPENAI_API_KEY", "OPENAI_BASE_URL", "CORP_LLM_TOKEN"}
			for _, key := range keys {
				t.Setenv(key, "unselected-shell-value")
			}
			configDir, cwd := t.TempDir(), t.TempDir()
			writeMinimalConfig(t, configDir, `
profiles:
  work:
    agent: claude
    config_dir_name: work
    credentials:
      corp: {backend: age, store: nonexistent, key: token, for: {claude: {env: [CORP_LLM_TOKEN]}}}
    default_credentials: [corp]
  personal:
    agent: codex
    config_dir_name: personal
default_profile: personal
`)
			mockExec := mocks.NewMockExecer(gomock.NewController(t))
			if !tc.explicit {
				mockExec.EXPECT().Exec(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
					func(binary string, args, env []string) error {
						innerBinary, innerArgs := unwrapSandbox(t, binary, args)
						if innerBinary != "/usr/local/bin/codex" {
							t.Errorf("binary = %q, want codex", innerBinary)
						}
						if !slices.Equal(innerArgs, []string{"/usr/local/bin/codex", "--profile", "deep-review"}) {
							t.Errorf("native arguments changed: %v", innerArgs)
						}
						if got, _ := envValue(env, "CODEX_HOME"); got != filepath.Join(home, ".codex-"+tc.profile) {
							t.Errorf("CODEX_HOME = %q, want selected profile home", got)
						}
						for _, key := range keys {
							if _, present := envValue(env, key); present {
								t.Errorf("unselected credential or endpoint %s reached Codex", key)
							}
						}
						return nil
					})
			}
			var stderr bytes.Buffer
			l := &Launcher{
				ConfigDir: configDir, Profile: tc.profile, Auth: tc.auth,
				Execer: mockExec, Stderr: &stderr, LookPath: okLookPath,
			}
			if tc.explicit {
				l.Credentials, l.CredentialsSet = []string{"corp"}, true
			}
			err := l.Launch(cwd, "codex", []string{"--profile", "deep-review"}, false, false, nil, nil)
			if tc.explicit {
				if !errors.Is(err, ErrCredentialIncompatible) {
					t.Fatalf("error = %v, want ErrCredentialIncompatible", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tc.wantNotice && !strings.Contains(stderr.String(), "codex uses its own login") {
				t.Errorf("missing native login notice: %s", stderr.String())
			}
		})
	}
}
