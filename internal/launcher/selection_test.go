package launcher

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/jskswamy/aide/internal/testutil"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"go.uber.org/mock/gomock"

	"github.com/jskswamy/aide/internal/config"
	aidectx "github.com/jskswamy/aide/internal/context"
	"github.com/jskswamy/aide/internal/launcher/mocks"
	"github.com/jskswamy/aide/internal/prefs"
	"github.com/jskswamy/aide/internal/provision"
	"github.com/jskswamy/aide/internal/trust"
)

func TestSelectHarness_Precedence(t *testing.T) {
	cfg := &config.Config{}
	saved := prefs.Preference{Harness: "codex"}
	repo := &config.ProjectOverride{Agent: "pi"}
	tests := []struct {
		name       string
		in         SelectionInputs
		po         *config.ProjectOverride
		agent      string // agent after Resolve (repository merged)
		wantAgent  string
		wantSource string
	}{
		{"flag beats all", SelectionInputs{Agent: "claude", Saved: saved}, repo, "pi", "claude", SourceFlag},
		{"saved beats repository", SelectionInputs{Saved: saved}, repo, "pi", "codex", SourcePreference},
		{"repository beats profile", SelectionInputs{}, repo, "pi", "pi", SourceRepository},
		{"profile default", SelectionInputs{}, nil, "claude", "claude", SourceProfile},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg.ProjectOverride = tt.po
			rc := &aidectx.ResolvedContext{Name: "work", Source: aidectx.SourceMatch, Context: config.Context{Agent: tt.agent}}
			sel, _, err := SelectHarness(cfg, rc, tt.in, okLookPath)
			if err != nil {
				t.Fatalf("SelectHarness() error = %v", err)
			}
			if rc.Context.Agent != tt.wantAgent || sel.HarnessSource != tt.wantSource {
				t.Errorf("got %s from %s, want %s from %s", rc.Context.Agent, sel.HarnessSource, tt.wantAgent, tt.wantSource)
			}
			if sel.ProfileSource != aidectx.SourceMatch {
				t.Errorf("ProfileSource = %q", sel.ProfileSource)
			}
		})
	}
}

func TestSelectHarness_NoticeWhenSavedDiffersFromRepository(t *testing.T) {
	cfg := &config.Config{ProjectOverride: &config.ProjectOverride{Agent: "pi"}, ProjectConfigPath: "/r/.aide.yaml"}
	rc := &aidectx.ResolvedContext{Name: "work", Context: config.Context{Agent: "pi"}}
	_, notice, err := SelectHarness(cfg, rc, SelectionInputs{Saved: prefs.Preference{Harness: "claude"}}, okLookPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(notice, `"claude"`) || !strings.Contains(notice, `"pi"`) {
		t.Errorf("notice = %q, want both harnesses named", notice)
	}

	rc = &aidectx.ResolvedContext{Name: "work", Context: config.Context{Agent: "pi"}}
	_, notice, _ = SelectHarness(cfg, rc, SelectionInputs{Saved: prefs.Preference{Harness: "pi"}}, okLookPath)
	if notice != "" {
		t.Errorf("no notice expected when saved equals repository, got %q", notice)
	}
}

func TestSelectHarness_UnknownSavedHarnessFails(t *testing.T) {
	cfg := &config.Config{}
	rc := &aidectx.ResolvedContext{Name: "work", Context: config.Context{Agent: "claude"}}
	_, _, err := SelectHarness(cfg, rc, SelectionInputs{Saved: prefs.Preference{Harness: "nosuch"}},
		func(string) (string, error) { return "", exec.ErrNotFound })
	if err == nil || !strings.Contains(err.Error(), "aide project") {
		t.Fatalf("error = %v, want a hint naming aide project", err)
	}
}

func TestSelectCredentials_Precedence(t *testing.T) {
	saved := prefs.Preference{Credentials: prefs.Selection("openrouter")}
	defaults := []string{"anthropic"}
	tests := []struct {
		name       string
		in         SelectionInputs
		want       []string
		wantSource string
	}{
		{"flag replaces saved and default", SelectionInputs{Credentials: []string{"openai"}, CredentialsSet: true, Saved: saved}, []string{"openai"}, SourceFlag},
		{"explicit empty flag", SelectionInputs{CredentialsSet: true, Saved: saved}, []string{}, SourceFlag},
		{"login injects nothing", SelectionInputs{Auth: AuthLogin, Saved: saved}, []string{}, SourceAuthLogin},
		{"saved replaces default", SelectionInputs{Saved: saved}, []string{"openrouter"}, SourcePreference},
		{"saved explicit empty", SelectionInputs{Saved: prefs.Preference{Credentials: prefs.Selection()}}, []string{}, SourcePreference},
		{"profile default", SelectionInputs{}, []string{"anthropic"}, SourceProfile},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var sel Selection
			sel.SelectCredentials(tt.in, defaults)
			if !slices.Equal(sel.Credentials, tt.want) || sel.CredentialSource != tt.wantSource {
				t.Errorf("got %v from %s, want %v from %s", sel.Credentials, sel.CredentialSource, tt.want, tt.wantSource)
			}
			if sel.Credentials == nil {
				t.Error("Credentials should be non-nil so JSON shows an explicit selection")
			}
		})
	}
}

func TestProjectProfileNotice(t *testing.T) {
	cfg := &config.Config{
		Contexts:          map[string]config.Context{"work": {}, "oss": {}},
		ProjectConfigPath: "/r/.aide.yaml",
		ProjectOverride:   &config.ProjectOverride{Profile: "oss"},
	}
	if n := ProjectProfileNotice(cfg, &aidectx.ResolvedContext{Name: "oss"}); n != "" {
		t.Errorf("no notice expected when the reference matches, got %q", n)
	}
	n := ProjectProfileNotice(cfg, &aidectx.ResolvedContext{Name: "work"})
	if !strings.Contains(n, "--profile oss") {
		t.Errorf("notice should suggest --profile oss: %q", n)
	}
	cfg.ProjectOverride.Profile = "ghost"
	n = ProjectProfileNotice(cfg, &aidectx.ResolvedContext{Name: "work"})
	if !strings.Contains(n, "does not define") || !strings.Contains(n, "oss, work") {
		t.Errorf("unknown-profile notice should list defined profiles: %q", n)
	}
	cfg.ProjectOverride = nil
	if n := ProjectProfileNotice(cfg, &aidectx.ResolvedContext{Name: "work"}); n != "" {
		t.Errorf("no notice without a reference, got %q", n)
	}
}

// okLookPath finds every binary.
func okLookPath(file string) (string, error) { return "/usr/local/bin/" + file, nil }

// gitRepo makes dir a git repository whose origin is remote.
func gitRepo(t *testing.T, dir, remote string) {
	t.Helper()
	for _, args := range [][]string{
		{"init", "-q", dir},
		{"-C", dir, "remote", "add", "origin", remote},
	} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

// selectionLaunch launches configYAML from cwd with l's selection
// fields and returns the agent env and stderr.
func selectionLaunch(t *testing.T, configYAML, cwd string, l *Launcher, agentFlag string) ([]string, string, error) {
	t.Helper()
	configDir := t.TempDir()
	writeMinimalConfig(t, configDir, configYAML)
	var env []string
	mockExec := mocks.NewMockExecer(gomock.NewController(t))
	mockExec.EXPECT().Exec(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ string, _ []string, e []string) error { env = e; return nil }).MaxTimes(1)
	var stderr bytes.Buffer
	l.Execer = mockExec
	l.ConfigDir = configDir
	l.Stderr = &stderr
	l.LookPath = okLookPath
	err := l.Launch(cwd, agentFlag, nil, false, false, nil, nil)
	return env, stderr.String(), err
}

func savePreference(t *testing.T, path string, key prefs.Key, p prefs.Preference) {
	t.Helper()
	if err := prefs.Update(path, func(f *prefs.File) error { f.Set(key, p); return nil }); err != nil {
		t.Fatal(err)
	}
}

const selectionConfig = `
agents:
  claude: {binary: claude}
  pi: {binary: pi}
  codex: {binary: codex}
profiles:
  work:
    agent: claude
  personal:
    agent: codex
    config_dir_name: personal
default_profile: work
`

func TestLauncher_SavedHarnessAppliesAndFlagWins(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cwd := t.TempDir()
	gitRepo(t, cwd, "git@github.com:org/repo.git")
	prefsPath := filepath.Join(t.TempDir(), "preferences.json")
	savePreference(t, prefsPath, prefs.Key{Profile: "work", Remote: "github.com/org/repo"}, prefs.Preference{Harness: "pi"})

	env, _, err := selectionLaunch(t, selectionConfig, cwd, &Launcher{PrefsPath: prefsPath}, "")
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if got, _ := envValue(env, "AIDE_AGENT"); got != "pi" {
		t.Errorf("AIDE_AGENT = %q, want the saved harness pi", got)
	}

	env, _, err = selectionLaunch(t, selectionConfig, cwd, &Launcher{PrefsPath: prefsPath}, "codex")
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if got, _ := envValue(env, "AIDE_AGENT"); got != "codex" {
		t.Errorf("AIDE_AGENT = %q, want --agent codex over the saved harness", got)
	}
}

func TestLauncher_PreferencesSharedAcrossWorktreesNotProfiles(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	main := t.TempDir()
	gitRepo(t, main, "https://github.com/org/repo.git")
	for _, args := range [][]string{
		{"-C", main, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "init"},
		{"-C", main, "worktree", "add", "-q", filepath.Join(main, "..", filepath.Base(main)+"-wt")},
	} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	worktree := filepath.Join(filepath.Dir(main), filepath.Base(main)+"-wt")
	t.Cleanup(func() { _ = os.RemoveAll(worktree) })
	// A separate clone with the ssh form of the same remote.
	clone := t.TempDir()
	gitRepo(t, clone, "ssh://git@github.com/org/repo.git")

	prefsPath := filepath.Join(t.TempDir(), "preferences.json")
	savePreference(t, prefsPath, prefs.Key{Profile: "work", Remote: "github.com/org/repo"}, prefs.Preference{Harness: "pi"})

	for _, dir := range []string{main, worktree, clone} {
		env, _, err := selectionLaunch(t, selectionConfig, dir, &Launcher{PrefsPath: prefsPath}, "")
		if err != nil {
			t.Fatalf("Launch(%s) error = %v", dir, err)
		}
		if got, _ := envValue(env, "AIDE_AGENT"); got != "pi" {
			t.Errorf("%s: AIDE_AGENT = %q, want the shared saved harness pi", dir, got)
		}
	}

	env, _, err := selectionLaunch(t, selectionConfig, worktree, &Launcher{PrefsPath: prefsPath, Profile: "personal"}, "")
	if err != nil {
		t.Fatalf("Launch(--profile personal) error = %v", err)
	}
	if got, _ := envValue(env, "AIDE_AGENT"); got != "codex" {
		t.Errorf("AIDE_AGENT = %q, want personal's default codex: preferences must not cross profiles", got)
	}
	if got, _ := envValue(env, "AIDE_PROFILE"); got != "personal" {
		t.Errorf("AIDE_PROFILE = %q, want personal", got)
	}
}

func TestLauncher_OneTimeFlagsNeverMutatePreferences(t *testing.T) {
	store, _ := fixtureStore(t)
	t.Setenv("HOME", t.TempDir())
	cwd := t.TempDir()
	gitRepo(t, cwd, "git@github.com:org/repo.git")
	prefsPath := filepath.Join(t.TempDir(), "preferences.json")
	savePreference(t, prefsPath, prefs.Key{Profile: "work", Remote: "github.com/org/repo"}, prefs.Preference{Harness: "claude"})
	before, _ := os.ReadFile(prefsPath)

	cfg := fmt.Sprintf(`
profiles:
  work:
    agent: claude
    credentials:
      anthropic: {backend: age, store: %[1]s, key: anthropic_api_key}
      openai: {backend: age, store: %[1]s, key: openai_api_key, harnesses: [claude, pi]}
    default_credentials: [anthropic]
default_profile: work
`, store)
	l := &Launcher{PrefsPath: prefsPath, Profile: "work", Credentials: []string{"openai"}, CredentialsSet: true, Auth: AuthAPI}
	if _, _, err := selectionLaunch(t, cfg, cwd, l, "pi"); err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if _, _, err := selectionLaunch(t, cfg, cwd, &Launcher{PrefsPath: prefsPath, Auth: AuthLogin}, ""); err != nil {
		t.Fatalf("Launch(login) error = %v", err)
	}
	after, _ := os.ReadFile(prefsPath)
	if !bytes.Equal(before, after) {
		t.Errorf("one-time flags changed preferences:\nbefore %s\nafter  %s", before, after)
	}
}

func TestLauncher_CredentialSelectionReplacesSavedAndDefault(t *testing.T) {
	store, values := fixtureStore(t)
	t.Setenv("HOME", t.TempDir())
	cwd := t.TempDir()
	gitRepo(t, cwd, "git@github.com:org/repo.git")
	prefsPath := filepath.Join(t.TempDir(), "preferences.json")
	key := prefs.Key{Profile: "work", Remote: "github.com/org/repo"}
	cfg := fmt.Sprintf(`
profiles:
  work:
    agent: claude
    credentials:
      anthropic: {backend: age, store: %[1]s, key: anthropic_api_key}
      openai: {backend: age, store: %[1]s, key: openai_api_key, harnesses: [claude]}
    default_credentials: [anthropic]
default_profile: work
`, store)

	// Saved selection replaces the profile default.
	savePreference(t, prefsPath, key, prefs.Preference{Credentials: prefs.Selection("openai")})
	env, _, err := selectionLaunch(t, cfg, cwd, &Launcher{PrefsPath: prefsPath}, "")
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if _, ok := envValue(env, "ANTHROPIC_API_KEY"); ok {
		t.Error("saved selection unioned with the profile default")
	}
	if got, _ := envValue(env, "OPENAI_API_KEY"); got != values["openai_api_key"] {
		t.Error("saved selection not injected")
	}

	// A launch selection replaces the saved one.
	env, _, err = selectionLaunch(t, cfg, cwd, &Launcher{PrefsPath: prefsPath, Credentials: []string{"anthropic"}, CredentialsSet: true}, "")
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if _, ok := envValue(env, "OPENAI_API_KEY"); ok {
		t.Error("--credential unioned with the saved selection")
	}
	if got, _ := envValue(env, "ANTHROPIC_API_KEY"); got != values["anthropic_api_key"] {
		t.Error("--credential selection not injected")
	}

	// An explicitly empty saved selection injects nothing.
	savePreference(t, prefsPath, key, prefs.Preference{Credentials: prefs.Selection()})
	env, _, err = selectionLaunch(t, cfg, cwd, &Launcher{PrefsPath: prefsPath}, "")
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if _, ok := envValue(env, "ANTHROPIC_API_KEY"); ok {
		t.Error("explicitly empty saved selection fell back to the profile default")
	}
}

func TestLauncher_SavedHarnessUnsupportedStopsBeforeDecryption(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cwd := t.TempDir()
	gitRepo(t, cwd, "git@github.com:org/repo.git")
	prefsPath := filepath.Join(t.TempDir(), "preferences.json")
	savePreference(t, prefsPath, prefs.Key{Profile: "work", Remote: "github.com/org/repo"}, prefs.Preference{Harness: "opencode"})

	decrypted := 0
	prev := loadSecretStore
	loadSecretStore = func(string) (map[string]string, error) { decrypted++; return map[string]string{"k": "v"}, nil }
	t.Cleanup(func() { loadSecretStore = prev })

	cfg := `
profiles:
  work:
    agent: claude
    config_dir_name: work
    secret: work
    credentials:
      anthropic: {backend: age, store: work, key: k}
    default_credentials: [anthropic]
default_profile: work
`
	_, _, err := selectionLaunch(t, cfg, cwd, &Launcher{PrefsPath: prefsPath}, "")
	if !errors.Is(err, provision.ErrProfileNotSupported) {
		t.Fatalf("Launch() error = %v, want ErrProfileNotSupported", err)
	}
	if decrypted != 0 {
		t.Errorf("decrypted %d stores before refusing the unsupported harness", decrypted)
	}
}

func TestLauncher_ExplicitProfileSelectsIdentity(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	env, _, err := selectionLaunch(t, selectionConfig, t.TempDir(), &Launcher{Profile: "personal"}, "")
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if got, _ := envValue(env, "AIDE_PROFILE"); got != "personal" {
		t.Errorf("AIDE_PROFILE = %q, want personal", got)
	}

	_, _, err = selectionLaunch(t, selectionConfig, t.TempDir(), &Launcher{Profile: "ghost"}, "")
	if err == nil || !strings.Contains(err.Error(), `"ghost"`) {
		t.Fatalf("Launch(--profile ghost) error = %v, want profile not found", err)
	}
}

func TestLauncher_NoRemoteUsesProfileDefaults(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	prefsPath := filepath.Join(t.TempDir(), "preferences.json")
	env, _, err := selectionLaunch(t, selectionConfig, t.TempDir(), &Launcher{PrefsPath: prefsPath}, "")
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if got, _ := envValue(env, "AIDE_AGENT"); got != "claude" {
		t.Errorf("AIDE_AGENT = %q, want the profile default", got)
	}
	if _, err := os.Stat(prefsPath); !errors.Is(err, os.ErrNotExist) {
		t.Error("a launch created the preferences file")
	}
}

func TestLauncher_ProjectProfileReferenceNeverSwitchesIdentity(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cwd := testutil.CanonicalTempDir(t)
	aidePath := filepath.Join(cwd, ".aide.yaml")
	content := []byte("profile: personal\n")
	if err := os.WriteFile(aidePath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	store := trustedStore(t, aidePath, content)
	env, stderr, err := selectionLaunch(t, selectionConfig, cwd, &Launcher{TrustStore: store}, "")
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if got, _ := envValue(env, "AIDE_PROFILE"); got != "work" {
		t.Errorf("AIDE_PROFILE = %q: .aide.yaml profile: switched identity", got)
	}
	if !strings.Contains(stderr, "--profile personal") {
		t.Errorf("expected a notice suggesting --profile personal:\n%s", stderr)
	}
	if strings.Count(stderr, "personal") > 2 {
		t.Errorf("notice printed more than once:\n%s", stderr)
	}
}

// trustedStore returns a trust store that trusts path with content.
func trustedStore(t *testing.T, path string, content []byte) *trust.Store {
	t.Helper()
	store := trust.NewStore(filepath.Join(t.TempDir(), "trust"))
	abs, _ := filepath.Abs(path)
	if err := store.Trust(abs, content); err != nil {
		t.Fatal(err)
	}
	return store
}

func TestPreviewSessionEnv_UsesSavedHarness(t *testing.T) {
	configDir, cwd, homeDir := t.TempDir(), t.TempDir(), t.TempDir()
	gitRepo(t, cwd, "git@github.com:org/repo.git")
	writeMinimalConfig(t, configDir, selectionConfig)
	savePreference(t, prefs.DefaultPath(homeDir), prefs.Key{Profile: "work", Remote: "github.com/org/repo"}, prefs.Preference{Harness: "pi"})
	cfg, err := config.Load(configDir, cwd)
	if err != nil {
		t.Fatal(err)
	}
	env, err := PreviewSessionEnv(cfg, cwd, "", homeDir, "")
	if err != nil {
		t.Fatalf("PreviewSessionEnv: %v", err)
	}
	if env["AIDE_AGENT"] != "pi" {
		t.Errorf("AIDE_AGENT = %q, want the saved harness pi a launch would use", env["AIDE_AGENT"])
	}
	// Naming the profile (statusline --profile work) keeps the saved
	// harness, as aide --profile work would.
	env, err = PreviewSessionEnv(cfg, cwd, "work", homeDir, "")
	if err != nil {
		t.Fatalf("PreviewSessionEnv(work): %v", err)
	}
	if env["AIDE_AGENT"] != "pi" {
		t.Errorf("named profile: AIDE_AGENT = %q, want the saved harness pi", env["AIDE_AGENT"])
	}
}

func TestLauncher_CorruptPreferencesStopWithRemedy(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cwd := t.TempDir()
	gitRepo(t, cwd, "git@github.com:org/repo.git")
	prefsPath := filepath.Join(t.TempDir(), "preferences.json")
	if err := os.WriteFile(prefsPath, []byte(`{not json`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err := selectionLaunch(t, selectionConfig, cwd, &Launcher{PrefsPath: prefsPath}, "")
	if !errors.Is(err, prefs.ErrCorrupt) {
		t.Fatalf("Launch() error = %v, want prefs.ErrCorrupt", err)
	}
	if !strings.Contains(err.Error(), prefsPath) || !strings.Contains(err.Error(), "aide project clear --reset") {
		t.Errorf("error should name the file and the remedy: %v", err)
	}
}

func TestLauncher_UntrustedProjectProfileReferencePrintsNothing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, ".aide.yaml"), []byte("profile: personal\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := trust.NewStore(filepath.Join(t.TempDir(), "trust"))
	env, stderr, err := selectionLaunch(t, selectionConfig, cwd, &Launcher{TrustStore: store}, "")
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if got, _ := envValue(env, "AIDE_PROFILE"); got != "work" {
		t.Errorf("AIDE_PROFILE = %q, want work", got)
	}
	if strings.Contains(stderr, "--profile personal") {
		t.Errorf("untrusted .aide.yaml produced a profile notice:\n%s", stderr)
	}
}

// aide project set writes, and a launch reads, the key PreferenceKey
// builds: the profile and the normalized origin remote.
func TestPreferenceKey_ProfileAndNormalizedOrigin(t *testing.T) {
	cwd := t.TempDir()
	gitRepo(t, cwd, "https://user:tok@github.com/org/repo.git")
	if got, want := PreferenceKey("work", aidectx.GitRemotes(cwd)), (prefs.Key{Profile: "work", Remote: "github.com/org/repo"}); got != want {
		t.Errorf("PreferenceKey = %+v, want %+v", got, want)
	}
	if got := PreferenceKey("work", nil); got != (prefs.Key{Profile: "work"}) {
		t.Errorf("PreferenceKey without remotes = %+v, want no remote", got)
	}
}
