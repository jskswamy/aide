package launcher

import (
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"go.uber.org/mock/gomock"

	"github.com/jskswamy/aide/internal/launcher/mocks"
)

// launchOpenCode launches with configYAML and args and returns the inner
// agent args and the environment Exec received. err is the launch error;
// on error Exec must not have been called.
func launchOpenCode(t *testing.T, configYAML, agentOverride string, args []string) (innerArgs, env []string, err error) {
	t.Helper()
	configDir := t.TempDir()
	cwd := t.TempDir()
	writeMinimalConfig(t, configDir, configYAML)

	ctrl := gomock.NewController(t)
	var capturedBinary string
	var capturedArgs []string
	mockExec := mocks.NewMockExecer(ctrl)
	mockExec.EXPECT().
		Exec(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(binary string, args []string, e []string) error {
			capturedBinary, capturedArgs, env = binary, args, e
			return nil
		}).
		MaxTimes(1)
	l := &Launcher{
		Execer:    mockExec,
		ConfigDir: configDir,
		LookPath: func(file string) (string, error) {
			return "/usr/local/bin/" + file, nil
		},
	}
	if err := l.Launch(cwd, agentOverride, args, false, false, nil, nil); err != nil {
		if capturedArgs != nil {
			t.Fatalf("Launch failed with %v after exec", err)
		}
		return nil, nil, err
	}
	_, innerArgs = unwrapSandbox(t, capturedBinary, capturedArgs)
	return innerArgs, env, nil
}

// An agents: alias whose binary is opencode gets the --standalone rule:
// the harness, not the alias name, decides.
func TestLauncher_OpenCodeAliasGetsStandalone(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	args, _, err := launchOpenCode(t, `
agents:
  oc:
    binary: opencode
profiles:
  work:
    agent: oc
default_profile: work
`, "", []string{"auth", "list"})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"/usr/local/bin/opencode", "auth", "list", "--standalone"}; !slices.Equal(args, want) {
		t.Errorf("args = %v, want %v", args, want)
	}
}

const openCodeWorkProfile = `
agents:
  claude:
    binary: claude
  opencode:
    binary: opencode
profiles:
  work:
    agent: opencode
    config_dir_name: work
default_profile: work
`

// openCodeHome isolates HOME, clears any OPENCODE_DB from the test
// process, and fails the test if launch setup creates a database file.
func openCodeHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	unsetEnvForTest(t, "OPENCODE_DB")
	t.Cleanup(func() {
		_ = filepath.WalkDir(home, func(path string, d fs.DirEntry, err error) error {
			if err == nil && strings.HasPrefix(d.Name(), "opencode.db") {
				t.Errorf("launch created %s", path)
			}
			return nil
		})
	})
	return home
}

func wantOpenCodeDB(t *testing.T, env []string, want string) {
	t.Helper()
	got, ok := envValue(env, "OPENCODE_DB")
	if !ok || got != want {
		t.Errorf("OPENCODE_DB = %q (set %v), want %q", got, ok, want)
	}
}

func wantNoOpenCodeDB(t *testing.T, env []string) {
	t.Helper()
	if got, ok := envValue(env, "OPENCODE_DB"); ok {
		t.Errorf("OPENCODE_DB = %q, want it unset", got)
	}
}

// A profile with a config dir opens its own database on launches that
// start a private server, and on debug paths, which reports it.
func TestLauncher_OpenCodeProfileDB(t *testing.T) {
	for name, tc := range map[string]struct {
		args, wantArgs []string
	}{
		"tui":         {nil, []string{"--standalone"}},
		"run":         {[]string{"run", "hi"}, []string{"run", "--standalone", "hi"}},
		"auth list":   {[]string{"auth", "list"}, []string{"auth", "list", "--standalone"}},
		"debug paths": {[]string{"debug", "paths"}, []string{"debug", "paths"}},
	} {
		t.Run(name, func(t *testing.T) {
			home := openCodeHome(t)
			args, env, err := launchOpenCode(t, openCodeWorkProfile, "", tc.args)
			if err != nil {
				t.Fatal(err)
			}
			wantOpenCodeDB(t, env, filepath.Join(home, ".opencode-work", "opencode.db"))
			if want := append([]string{"/usr/local/bin/opencode"}, tc.wantArgs...); !slices.Equal(args, want) {
				t.Errorf("args = %v, want %v", args, want)
			}
		})
	}
}

// Commands that use or manage the shared background server must not
// start it with a profile database.
func TestLauncher_OpenCodeSharedServerCommandsGetNoDB(t *testing.T) {
	for _, args := range [][]string{{"service", "stop"}, {"mcp", "list"}, {"serve"}, {"plugin", "list"}, {"--server", "http://x"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			openCodeHome(t)
			_, env, err := launchOpenCode(t, openCodeWorkProfile, "", args)
			if err != nil {
				t.Fatal(err)
			}
			wantNoOpenCodeDB(t, env)
		})
	}
}

func TestLauncher_OpenCodeConfigDirsWins(t *testing.T) {
	for name, dir := range map[string]string{"own dir": "oc-login", "global pin": ".config/opencode"} {
		t.Run(name, func(t *testing.T) {
			home := openCodeHome(t)
			_, env, err := launchOpenCode(t, `
profiles:
  work:
    agent: opencode
    config_dir_name: work
    config_dirs:
      opencode: ~/`+dir+`
default_profile: work
`, "", nil)
			if err != nil {
				t.Fatal(err)
			}
			wantOpenCodeDB(t, env, filepath.Join(home, dir, "opencode.db"))
		})
	}
}

// Without a selected config dir the launch neither sets nor rewrites
// OPENCODE_DB, even a relative value.
func TestLauncher_OpenCodeNoConfigDirLeavesAmbientDB(t *testing.T) {
	openCodeHome(t)
	t.Setenv("OPENCODE_DB", "relative.db")
	_, env, err := launchOpenCode(t, "profiles:\n  me: {agent: opencode}\ndefault_profile: me\n", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	wantOpenCodeDB(t, env, "relative.db")
}

func TestLauncher_OpenCodeProfileDBOverridesEarlierValues(t *testing.T) {
	home := openCodeHome(t)
	t.Setenv("OPENCODE_DB", "relative.db")
	_, env, err := launchOpenCode(t, `
profiles:
  work:
    agent: opencode
    config_dir_name: work
    env:
      OPENCODE_DB: /elsewhere/opencode.db
default_profile: work
`, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	wantOpenCodeDB(t, env, filepath.Join(home, ".opencode-work", "opencode.db"))
	if n := strings.Count(strings.Join(env, "\n"), "OPENCODE_DB="); n != 1 {
		t.Errorf("OPENCODE_DB appears %d times in the exec env", n)
	}
}

func TestLauncher_OpenCodeProfileDBErrorsBeforeExec(t *testing.T) {
	for blocked, want := range map[string]string{
		"OPENCODE_DB":         "opencode profile database path is blocked by never_allow_env",
		"OPENCODE_CONFIG_DIR": "opencode profile database path is not absolute: ",
	} {
		t.Run(blocked, func(t *testing.T) {
			openCodeHome(t)
			_, _, err := launchOpenCode(t, openCodeWorkProfile+"never_allow_env: ["+blocked+"]\n", "", nil)
			if err == nil || err.Error() != want {
				t.Errorf("err = %v, want %q", err, want)
			}
		})
	}
}

func TestLauncher_OpenCodeProfileDBOnlyForOpenCode(t *testing.T) {
	openCodeHome(t)
	_, env, err := launchOpenCode(t, strings.Replace(openCodeWorkProfile, "agent: opencode", "agent: claude", 1), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	wantNoOpenCodeDB(t, env)
}

// The harness, not the agent name, decides: an alias whose binary is
// opencode and --agent opencode on a Claude profile both qualify.
func TestLauncher_OpenCodeProfileDBFollowsHarness(t *testing.T) {
	for name, tc := range map[string]struct{ cfg, override string }{
		"alias": {`
agents:
  oc:
    binary: opencode
profiles:
  work:
    agent: oc
    config_dir_name: work
default_profile: work
`, ""},
		"agent override": {strings.Replace(openCodeWorkProfile, "agent: opencode", "agent: claude", 1), "opencode"},
	} {
		t.Run(name, func(t *testing.T) {
			home := openCodeHome(t)
			args, env, err := launchOpenCode(t, tc.cfg, tc.override, []string{"auth", "list"})
			if err != nil {
				t.Fatal(err)
			}
			wantOpenCodeDB(t, env, filepath.Join(home, ".opencode-work", "opencode.db"))
			if !slices.Contains(args, "--standalone") {
				t.Errorf("args = %v, want --standalone", args)
			}
		})
	}
}

// clean_env filters the sandboxed env; the database path is set after
// that filter, so it survives.
func TestLauncher_OpenCodeProfileDBSurvivesCleanEnv(t *testing.T) {
	home := openCodeHome(t)
	_, env, err := launchOpenCode(t, `
profiles:
  work:
    agent: opencode
    config_dir_name: work
    sandbox:
      clean_env: true
default_profile: work
`, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	wantOpenCodeDB(t, env, filepath.Join(home, ".opencode-work", "opencode.db"))
}

func TestOpenCodeProfileDBRejectsRelativeConfigDir(t *testing.T) {
	_, err := openCodeProfileDB([]string{"OPENCODE_CONFIG_DIR=rel"}, nil)
	if want := "opencode profile database path is not absolute: rel/opencode.db"; err == nil || err.Error() != want {
		t.Errorf("err = %v, want %q", err, want)
	}
}
