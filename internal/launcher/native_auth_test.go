package launcher

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeClaudeSettings(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestClaudeAPIKeyHelperNotice(t *testing.T) {
	for name, tc := range map[string]struct {
		body string // "" writes no file
		want bool
	}{
		"helper set":       {body: `{"apiKeyHelper": "/opt/vault-key --token s3cret"}`, want: true},
		"no helper":        {body: `{"model": "opus"}`},
		"empty helper":     {body: `{"apiKeyHelper": ""}`},
		"malformed json":   {body: `{"apiKeyHelper": `},
		"no settings.json": {},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.body != "" {
				writeClaudeSettings(t, dir, tc.body)
			}
			got := claudeAPIKeyHelperNotice(dir)
			if (got != "") != tc.want {
				t.Fatalf("notice = %q, want notice=%v", got, tc.want)
			}
			if strings.Contains(got, "s3cret") || strings.Contains(got, "vault-key") {
				t.Errorf("notice leaks the helper command: %q", got)
			}
			if tc.want && (!strings.Contains(got, "apiKeyHelper") || !strings.Contains(got, filepath.Join(dir, "settings.json"))) {
				t.Errorf("notice should name apiKeyHelper and the settings file: %q", got)
			}
		})
	}
}

// A Claude profile launch whose own settings.json defines apiKeyHelper
// prints a notice naming the setting, never its command.
func TestLauncher_ProfileLaunchNoticesAPIKeyHelper(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeClaudeSettings(t, filepath.Join(home, ".claude-work"), `{"apiKeyHelper": "/opt/vault-key --token s3cret"}`)

	_, stderr := launchCapturingEnvAndStderr(t, `
agents:
  claude:
    binary: claude
profiles:
  work:
    agent: claude
    config_dir_name: work
default_profile: work
`, "")

	if !strings.Contains(stderr, "apiKeyHelper") {
		t.Errorf("no apiKeyHelper notice on stderr:\n%s", stderr)
	}
	if strings.Contains(stderr, "s3cret") {
		t.Errorf("stderr leaks the helper command:\n%s", stderr)
	}
}

// The global ~/.claude/settings.json is not read by a profile launch,
// and a launch without a profile dir gets no notice.
func TestLauncher_NoAPIKeyHelperNoticeOutsideProfileDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeClaudeSettings(t, filepath.Join(home, ".claude"), `{"apiKeyHelper": "/opt/vault-key"}`)

	for _, cfg := range []string{`
agents:
  claude:
    binary: claude
profiles:
  work:
    agent: claude
    config_dir_name: work
default_profile: work
`, `
agents:
  claude:
    binary: claude
profiles:
  plain:
    agent: claude
default_profile: plain
`} {
		_, stderr := launchCapturingEnvAndStderr(t, cfg, "")
		if strings.Contains(stderr, "apiKeyHelper") {
			t.Errorf("unexpected apiKeyHelper notice:\n%s", stderr)
		}
	}
}
