package launcher

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/mock/gomock"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/launcher/mocks"
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

func TestNativeAuthNotice_OnlyClaudeProfileDirs(t *testing.T) {
	home := t.TempDir()
	helper := `{"apiKeyHelper": "/opt/vault-key"}`
	writeClaudeSettings(t, filepath.Join(home, ".claude-work"), helper)
	writeClaudeSettings(t, filepath.Join(home, ".codex-work"), helper)
	writeClaudeSettings(t, filepath.Join(home, ".claude"), helper)

	work := config.Context{Profile: "work"}
	for name, tc := range map[string]struct {
		harness string
		ctx     config.Context
		want    bool
	}{
		"claude profile":       {"claude", work, true},
		"codex profile":        {"codex", work, false},
		"claude without a dir": {"claude", config.Context{}, false},
		"unknown harness":      {"my-wrapper", work, false},
	} {
		t.Run(name, func(t *testing.T) {
			if got := nativeAuthNotice(tc.harness, tc.ctx, home); (got != "") != tc.want {
				t.Errorf("notice = %q, want notice=%v", got, tc.want)
			}
		})
	}
}

// A user-defined agent whose binary is claude is checked like claude:
// the launcher passes cfg.HarnessOf(agent).
func TestNativeAuthNotice_AgentAliasOfClaude(t *testing.T) {
	home := t.TempDir()
	writeClaudeSettings(t, filepath.Join(home, ".claude-work"), `{"apiKeyHelper": "/opt/vault-key"}`)
	cfg := &config.Config{Agents: map[string]config.AgentDef{"work-claude": {Binary: "/usr/local/bin/claude"}}}
	ctx := config.Context{Agent: "work-claude", Profile: "work"}
	if got := nativeAuthNotice(cfg.HarnessOf(ctx.Agent), ctx, home); !strings.Contains(got, "apiKeyHelper") {
		t.Errorf("no notice for an agent alias of claude: %q", got)
	}
}

// Two profiles on Claude's global login would share it: launching
// either is refused, while a profile with its own dir still launches.
func TestLauncher_RefusesProfileSharingGlobalLogin(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	configDir, cwd := t.TempDir(), t.TempDir()
	writeMinimalConfig(t, configDir, `
agents:
  claude:
    binary: claude
profiles:
  work:
    agent: claude
  personal:
    agent: claude
  oss:
    agent: claude
    config_dir_name: oss
default_profile: work
`)
	l := &Launcher{ConfigDir: configDir, Stderr: &bytes.Buffer{},
		LookPath: func(file string) (string, error) { return "/usr/local/bin/" + file, nil }}
	err := l.Launch(cwd, "", nil, false, false, nil, nil)
	if !errors.Is(err, config.ErrProfileDirAlias) {
		t.Fatalf("Launch err = %v, want ErrProfileDirAlias", err)
	}
	if !strings.Contains(err.Error(), `"personal"`) {
		t.Errorf("error does not name the other profile: %v", err)
	}

	ctrl := gomock.NewController(t)
	mockExec := mocks.NewMockExecer(ctrl)
	mockExec.EXPECT().Exec(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
	l.Execer, l.Profile = mockExec, "oss"
	if err := l.Launch(cwd, "", nil, false, false, nil, nil); err != nil {
		t.Fatalf("isolated profile: Launch err = %v", err)
	}
}

// The refusal follows the launch's final harness: two aider profiles
// may not both run Claude from ~/.claude through --agent claude.
func TestLauncher_RefusesAgentOverrideOntoSharedGlobalLogin(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	configDir, cwd := t.TempDir(), t.TempDir()
	writeMinimalConfig(t, configDir, `
agents:
  aider: {binary: aider}
  claude: {binary: claude}
profiles:
  a: {agent: aider}
  b: {agent: aider}
default_profile: a
`)
	l := &Launcher{ConfigDir: configDir, Stderr: &bytes.Buffer{},
		LookPath: func(file string) (string, error) { return "/usr/local/bin/" + file, nil }}
	if err := l.Launch(cwd, "claude", nil, false, false, nil, nil); !errors.Is(err, config.ErrProfileDirAlias) {
		t.Fatalf("Launch --agent claude: err = %v, want ErrProfileDirAlias", err)
	}
}
