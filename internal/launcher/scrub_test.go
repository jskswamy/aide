package launcher

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/mock/gomock"

	"github.com/jskswamy/aide/internal/launcher/mocks"
)

// launchCapturingEnvAndStderr is launchCapturingEnv that also returns
// what the launch wrote to stderr.
func launchCapturingEnvAndStderr(t *testing.T, configYAML, agentOverride string) ([]string, string) {
	t.Helper()
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
		})
	var stderr bytes.Buffer
	l := &Launcher{
		Execer:    mockExec,
		ConfigDir: configDir,
		Stderr:    &stderr,
		LookPath: func(file string) (string, error) {
			return "/usr/local/bin/" + file, nil
		},
	}
	if err := l.Launch(cwd, agentOverride, nil, false, false, nil, nil); err != nil {
		t.Fatalf("Launch failed: %v", err)
	}
	return capturedEnv, stderr.String()
}

func setShellCredentials(t *testing.T) {
	t.Helper()
	for k, v := range map[string]string{
		"ANTHROPIC_API_KEY":     "sk-ant-shell",
		"OPENAI_API_KEY":        "sk-openai-shell",
		"CODEX_API_KEY":         "codex-shell",
		"SOPS_AGE_KEY":          "AGE-SECRET-KEY-SHELL",
		"SOPS_AGE_KEY_FILE":     "/shell/age.txt",
		"AWS_SECRET_ACCESS_KEY": "aws-shell",
		"AWS_PROFILE":           "prod",
	} {
		t.Setenv(k, v)
	}
}

func TestLauncher_ScrubsInheritedCredentials(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	setShellCredentials(t)

	env, stderr := launchCapturingEnvAndStderr(t, `
agents:
  claude:
    binary: claude
contexts:
  work:
    agent: claude
    env:
      FOO: bar
      OPENAI_API_KEY: from-context
default_context: work
`, "")

	for _, k := range []string{"ANTHROPIC_API_KEY", "CODEX_API_KEY", "SOPS_AGE_KEY", "SOPS_AGE_KEY_FILE", "AWS_SECRET_ACCESS_KEY", "AWS_PROFILE"} {
		if v, ok := envValue(env, k); ok {
			t.Errorf("inherited %s reached the agent: %q", k, v)
		}
	}
	if v, _ := envValue(env, "FOO"); v != "bar" {
		t.Errorf("FOO = %q, want bar", v)
	}
	if v, _ := envValue(env, "OPENAI_API_KEY"); v != "from-context" {
		t.Errorf("context env OPENAI_API_KEY = %q, want from-context", v)
	}
	var warnings []string
	for _, line := range strings.Split(stderr, "\n") {
		if strings.Contains(line, "warning") {
			warnings = append(warnings, line)
		}
	}
	warned := strings.Join(warnings, "\n")
	if !strings.Contains(warned, "OPENAI_API_KEY") {
		t.Errorf("no migration warning for env: OPENAI_API_KEY; stderr:\n%s", stderr)
	}
	if strings.Contains(warned, "from-context") {
		t.Errorf("warning printed the credential value: %s", warned)
	}
	if strings.Contains(warned, "FOO") {
		t.Errorf("warning flagged a non-credential name: %s", warned)
	}
}

func TestLauncher_ScrubsMinimalConfigAndCustomNames(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	setShellCredentials(t)
	t.Setenv("MY_PROVIDER_TOKEN", "custom-shell")
	t.Setenv("CORP_LLM_KEY", "corp-shell")
	t.Setenv("UNRELATED", "kept")

	env, _ := launchCapturingEnvAndStderr(t, `
agent: claude
scrub_env:
  - MY_PROVIDER_TOKEN
  - CORP_LLM_*
`, "")

	for _, k := range []string{"ANTHROPIC_API_KEY", "SOPS_AGE_KEY", "MY_PROVIDER_TOKEN", "CORP_LLM_KEY"} {
		if v, ok := envValue(env, k); ok {
			t.Errorf("inherited %s reached the agent: %q", k, v)
		}
	}
	if v, _ := envValue(env, "UNRELATED"); v != "kept" {
		t.Errorf("UNRELATED = %q, want kept", v)
	}
}

func TestLauncher_ConfigDirRedirectKeptWithoutProfile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEX_HOME", "/global/codex")

	env, _ := launchCapturingEnvAndStderr(t, `
agents:
  claude:
    binary: claude
contexts:
  work:
    agent: claude
default_context: work
`, "")

	if v, _ := envValue(env, "CODEX_HOME"); v != "/global/codex" {
		t.Errorf("CODEX_HOME = %q, want the shell value kept without a profile", v)
	}
}

func TestLauncher_ConfigDirRedirectsScrubbedWithProfile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "/global/claude")
	t.Setenv("CODEX_HOME", "/global/codex")
	t.Setenv("PI_CODING_AGENT_DIR", "/global/pi")

	env, _ := launchCapturingEnvAndStderr(t, `
agents:
  claude:
    binary: claude
profiles:
  work:
    agent: claude
    config_dir_name: work
default_profile: work
`, "")

	if v, _ := envValue(env, "CLAUDE_CONFIG_DIR"); v != filepath.Join(home, ".claude-work") {
		t.Errorf("CLAUDE_CONFIG_DIR = %q, want the profile dir", v)
	}
	for _, k := range []string{"CODEX_HOME", "PI_CODING_AGENT_DIR"} {
		if v, ok := envValue(env, k); ok {
			t.Errorf("shell %s reached a profile launch: %q", k, v)
		}
	}
}

func TestLauncher_CapabilityEnvAllowReadmitsCloudVariables(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	setShellCredentials(t)

	env, _ := launchCapturingEnvAndStderr(t, `
agents:
  claude:
    binary: claude
contexts:
  work:
    agent: claude
    capabilities: [aws]
default_context: work
`, "")

	if v, _ := envValue(env, "AWS_PROFILE"); v != "prod" {
		t.Errorf("AWS_PROFILE = %q, want prod (granted by the aws capability)", v)
	}
	if v, ok := envValue(env, "ANTHROPIC_API_KEY"); ok {
		t.Errorf("ANTHROPIC_API_KEY reached the agent: %q", v)
	}
}

func TestPassthrough_ScrubsOnlySecretMaterial(t *testing.T) {
	requireClaudeHome(t)
	setShellCredentials(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	ctrl := gomock.NewController(t)
	var env []string
	mockExec := mocks.NewMockExecer(ctrl)
	mockExec.EXPECT().
		Exec(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ string, _ []string, e []string) error {
			env = e
			return nil
		})
	l := &Launcher{
		Execer:   mockExec,
		Stderr:   &bytes.Buffer{},
		LookPath: mockLookPath(map[string]string{"claude": "/usr/local/bin/claude"}),
	}
	if err := l.Passthrough(t.TempDir(), "", nil); err != nil {
		t.Fatalf("Passthrough failed: %v", err)
	}

	for _, k := range []string{"SOPS_AGE_KEY", "SOPS_AGE_KEY_FILE"} {
		if v, ok := envValue(env, k); ok {
			t.Errorf("%s reached the passthrough agent: %q", k, v)
		}
	}
	if v, _ := envValue(env, "ANTHROPIC_API_KEY"); v != "sk-ant-shell" {
		t.Errorf("ANTHROPIC_API_KEY = %q, want the shell value (passthrough is not isolation)", v)
	}
}

func TestLauncher_ScrubEnvBeatsCapabilityEnvAllow(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	setShellCredentials(t)

	env, _ := launchCapturingEnvAndStderr(t, `
agents:
  claude:
    binary: claude
scrub_env: [AWS_PROFILE]
contexts:
  work:
    agent: claude
    capabilities: [aws]
default_context: work
`, "")

	if v, ok := envValue(env, "AWS_PROFILE"); ok {
		t.Errorf("AWS_PROFILE re-admitted despite scrub_env: %q", v)
	}
}

func TestLauncher_LegacyCredentialWarningNamesProfile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for name, tc := range map[string]struct{ yaml, want string }{
		"profiles": {`
agents:
  claude:
    binary: claude
profiles:
  work:
    agent: claude
    env:
      OPENAI_API_KEY: x
default_profile: work
`, `profile "work"`},
		"contexts": {`
agents:
  claude:
    binary: claude
contexts:
  work:
    agent: claude
    env:
      OPENAI_API_KEY: x
default_context: work
`, `profile (context) "work"`},
	} {
		t.Run(name, func(t *testing.T) {
			_, stderr := launchCapturingEnvAndStderr(t, tc.yaml, "")
			if !strings.Contains(stderr, "warning: "+tc.want+" ") {
				t.Errorf("warning does not name %s; stderr:\n%s", tc.want, stderr)
			}
		})
	}
}
