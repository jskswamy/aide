package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// loadYAML writes body as config.yaml in a fresh dir and loads it with
// an empty project dir and HOME pointed at a temp dir.
func loadYAML(t *testing.T, body string) (*Config, error) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	configDir := t.TempDir()
	writeFile(t, filepath.Join(configDir, "config.yaml"), body)
	return Load(configDir, t.TempDir())
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoad_LegacyContextsKeepNativeDirAssociations(t *testing.T) {
	cfg, err := loadYAML(t, `contexts:
  work:
    agent: claude
    profile: acme
    profile_dir: ~/.claude-acme
default_context: work
`)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	ctx := cfg.Contexts["work"]
	if ctx.Profile != "acme" || ctx.ProfileDir != "~/.claude-acme" {
		t.Errorf("native dir association = %q/%q, want acme/~/.claude-acme", ctx.Profile, ctx.ProfileDir)
	}
	if cfg.DefaultContext != "work" {
		t.Errorf("DefaultContext = %q, want work", cfg.DefaultContext)
	}
	if cfg.ProfileSchema {
		t.Error("ProfileSchema = true for a contexts: file")
	}
}

func TestLoad_ProfilesNormalizeIntoContexts(t *testing.T) {
	cfg, err := loadYAML(t, `profiles:
  work:
    agent: claude
    config_dir_name: acme
    config_dir: ~/.claude-acme
    sandbox:
      profile: strict
  personal:
    agent: codex
default_profile: personal
`)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	work := cfg.Contexts["work"]
	if work.Profile != "acme" || work.ProfileDir != "~/.claude-acme" {
		t.Errorf("work native dir = %q/%q, want acme/~/.claude-acme", work.Profile, work.ProfileDir)
	}
	if work.Sandbox == nil || work.Sandbox.ProfileName != "strict" {
		t.Errorf("work sandbox policy = %+v, want named policy strict", work.Sandbox)
	}
	if cfg.Contexts["personal"].Agent != "codex" {
		t.Errorf("personal agent = %q, want codex", cfg.Contexts["personal"].Agent)
	}
	if cfg.DefaultContext != "personal" {
		t.Errorf("DefaultContext = %q, want personal", cfg.DefaultContext)
	}
	if !cfg.ProfileSchema {
		t.Error("ProfileSchema = false for a profiles: file")
	}
}

func TestLoad_MixedDeclarationsFail(t *testing.T) {
	cases := map[string]struct {
		body string
		keys []string
	}{
		"both maps": {
			body: "contexts:\n  a: {agent: claude}\nprofiles:\n  b: {agent: claude}\n",
			keys: []string{"contexts:", "profiles:"},
		},
		"both defaults": {
			body: "default_context: a\ndefault_profile: a\n",
			keys: []string{"default_context:", "default_profile:"},
		},
		"new default with legacy map": {
			body: "contexts:\n  a: {agent: claude}\ndefault_profile: a\n",
			keys: []string{"contexts:", "default_profile:"},
		},
		"legacy default with new map": {
			body: "profiles:\n  a: {agent: claude}\ndefault_context: a\n",
			keys: []string{"profiles:", "default_context:"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := loadYAML(t, tc.body)
			if !errors.Is(err, ErrMixedProfileDeclarations) {
				t.Fatalf("Load() error = %v, want ErrMixedProfileDeclarations", err)
			}
			for _, k := range tc.keys {
				if !strings.Contains(err.Error(), k) {
					t.Errorf("error %q does not name %s", err, k)
				}
			}
		})
	}
}

func TestLoad_ProfilesRejectNestedLegacyDirKeys(t *testing.T) {
	cases := map[string]string{
		"profile:":     "config_dir_name:",
		"profile_dir:": "config_dir:",
	}
	for legacy, replacement := range cases {
		t.Run(legacy, func(t *testing.T) {
			_, err := loadYAML(t, "profiles:\n  work:\n    agent: claude\n    "+legacy+" acme\n")
			if err == nil {
				t.Fatal("Load() succeeded, want error")
			}
			for _, want := range []string{"work", legacy, replacement} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not mention %s", err, want)
				}
			}
		})
	}
}

func TestLoad_ContextsRejectProfileOnlyDirKeys(t *testing.T) {
	_, err := loadYAML(t, "contexts:\n  work:\n    agent: claude\n    config_dir_name: acme\n")
	if err == nil {
		t.Fatal("Load() succeeded, want error")
	}
	for _, want := range []string{"config_dir_name:", "profiles:", "profile:"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %s", err, want)
		}
	}
}

func TestWriteConfig_KeepsLoadedSchema(t *testing.T) {
	cases := map[string]struct {
		body        string
		want, avoid []string
	}{
		"profiles": {
			body:  "profiles:\n  work:\n    agent: claude\n    config_dir_name: acme\n    config_dir: ~/.claude-acme\n    config_dirs:\n      codex: ~/.codex-acme\n    sandbox:\n      profile: strict\ndefault_profile: work\n",
			want:  []string{"profiles:", "default_profile: work", "config_dir_name: acme", "config_dir: ~/.claude-acme", "profile: strict", "codex: ~/.codex-acme"},
			avoid: []string{"contexts:", "default_context:", "profile: acme", "profile_dir:"},
		},
		"contexts": {
			body:  "contexts:\n  work:\n    agent: claude\n    profile: acme\ndefault_context: work\n",
			want:  []string{"contexts:", "default_context: work", "profile: acme"},
			avoid: []string{"profiles:", "default_profile:", "config_dir"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			cfg, err := loadYAML(t, tc.body)
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			out, err := yaml.Marshal(cfg)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}
			for _, w := range tc.want {
				if !strings.Contains(string(out), w) {
					t.Errorf("output lacks %q:\n%s", w, out)
				}
			}
			for _, a := range tc.avoid {
				if strings.Contains(string(out), a) {
					t.Errorf("output contains %q:\n%s", a, out)
				}
			}
			var back Config
			if err := yaml.Unmarshal(out, &back); err != nil {
				t.Fatalf("re-reading output: %v", err)
			}
			if back.Contexts["work"].Profile != "acme" {
				t.Errorf("round trip lost native dir name: %+v", back.Contexts["work"])
			}
		})
	}
}

func TestLoad_ProfilesSharingResolvedDirFail(t *testing.T) {
	cases := map[string]func(t *testing.T, home string) (string, string){
		"symlinked dir": func(t *testing.T, home string) (string, string) {
			mkdir(t, filepath.Join(home, "real"))
			symlink(t, filepath.Join(home, "real"), filepath.Join(home, "link"))
			return "~/real", "~/link"
		},
		"not yet created": func(t *testing.T, home string) (string, string) {
			mkdir(t, filepath.Join(home, "real"))
			symlink(t, filepath.Join(home, "real"), filepath.Join(home, "link"))
			return "~/real/claude", "~/link/claude"
		},
		"same explicit dir": func(*testing.T, string) (string, string) {
			return "~/.claude-shared", "~/.claude-shared"
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			a, b := setup(t, home)
			configDir := t.TempDir()
			writeFile(t, filepath.Join(configDir, "config.yaml"), "profiles:\n"+
				"  a:\n    agent: claude\n    config_dir_name: a\n    config_dir: "+a+"\n"+
				"  b:\n    agent: claude\n    config_dir_name: b\n    config_dir: "+b+"\n")
			_, err := Load(configDir, t.TempDir())
			if !errors.Is(err, ErrProfileDirAlias) {
				t.Fatalf("Load() error = %v, want ErrProfileDirAlias", err)
			}
			for _, want := range []string{`"a"`, `"b"`, "config_dir"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not mention %s", err, want)
				}
			}
		})
	}
}

func TestLoad_CrossAgentProfilesSharingDirExplainConfigDirsRemedy(t *testing.T) {
	_, err := loadYAML(t, "contexts:\n"+
		"  work-claude:\n    agent: claude\n    profile: work\n"+
		"  work-codex:\n    agent: codex\n    profile: work\n")
	if !errors.Is(err, ErrProfileDirAlias) {
		t.Fatalf("Load() error = %v, want ErrProfileDirAlias", err)
	}
	for _, want := range []string{
		`"work-claude" (agent claude)`, `"work-codex" (agent codex)`, "--agent", "merge",
		`profile: work-codex, config_dirs: {codex: ~/.codex-work} on "work-codex"`,
		`config_dirs: {codex: ~/.codex-work-claude} on "work-claude"`,
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %s", err, want)
		}
	}
	if strings.Contains(err.Error(), "profile_dir:") {
		t.Errorf("error %q suggests the all-harness pin", err)
	}

	// Following the suggested remedy loads and keeps both logins.
	cfg, err := loadYAML(t, "contexts:\n"+
		"  work-claude:\n    agent: claude\n    profile: work\n    config_dirs: {codex: ~/.codex-work-claude}\n"+
		"  work-codex:\n    agent: codex\n    profile: work-codex\n    config_dirs: {codex: ~/.codex-work}\n")
	if err != nil {
		t.Fatalf("remedied config: Load() error = %v", err)
	}
	home, _ := os.UserHomeDir()
	if got := cfg.Contexts["work-claude"].NativeConfigDir("claude", home); got != filepath.Join(home, ".claude-work") {
		t.Errorf("work-claude claude dir = %q, want its existing login dir", got)
	}
	if got := cfg.Contexts["work-codex"].NativeConfigDir("codex", home); got != filepath.Join(home, ".codex-work") {
		t.Errorf("work-codex codex dir = %q, want its existing login dir", got)
	}
}

func TestContextNativeConfigDirResolutionOrder(t *testing.T) {
	home := "/home/u"
	ctx := Context{Profile: "work", ProfileDir: "~/pinned", ConfigDirs: map[string]string{"claude": "~/claude-only"}}
	if got := ctx.NativeConfigDir("claude", home); got != "/home/u/claude-only" {
		t.Errorf("map entry: got %q", got)
	}
	if got := ctx.NativeConfigDir("codex", home); got != "/home/u/pinned" {
		t.Errorf("single pin: got %q", got)
	}
	ctx.ProfileDir = ""
	if got := ctx.NativeConfigDir("codex", home); got != "/home/u/.codex-work" {
		t.Errorf("derived: got %q", got)
	}
}

func TestLoad_InvalidConfigDirsFail(t *testing.T) {
	cases := map[string]struct{ entry, want string }{
		"unknown harness":      {"config_dir_name: w\n    config_dirs: {nosuch: ~/x}", "nosuch"},
		"harness w/o profiles": {"config_dir_name: w\n    config_dirs: {opencode: ~/x}", "opencode"},
		"outside home":         {"config_dir_name: w\n    config_dirs: {claude: /opt/x}", "$HOME"},
		"empty path":           {"config_dir_name: w\n    config_dirs: {claude: \"\"}", "claude"},
		"without dir name":     {"config_dirs: {claude: ~/x}", "config_dir_name"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := loadYAML(t, "profiles:\n  work:\n    agent: claude\n    "+tc.entry+"\n")
			if !errors.Is(err, ErrInvalidConfigDirs) {
				t.Fatalf("Load() error = %v, want ErrInvalidConfigDirs", err)
			}
			if !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "work") {
				t.Errorf("error %q should name profile work and %s", err, tc.want)
			}
		})
	}
}

func TestLoad_ExistingDirsComparedByIdentity(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	mkdir(t, filepath.Join(home, "real"))
	symlink(t, filepath.Join(home, "real"), filepath.Join(home, "link"))
	configDir := t.TempDir()
	writeFile(t, filepath.Join(configDir, "config.yaml"), "profiles:\n"+
		"  a:\n    agent: claude\n    config_dir_name: a\n    config_dir: ~/real\n"+
		"  b:\n    agent: claude\n    config_dir_name: b\n    config_dir: ~/link\n")
	resolveNearest := resolveDir
	resolveDir = func(p string) string { return p } // path comparison alone would miss this
	t.Cleanup(func() { resolveDir = resolveNearest })
	if _, err := Load(configDir, t.TempDir()); !errors.Is(err, ErrProfileDirAlias) {
		t.Fatalf("Load() error = %v, want ErrProfileDirAlias via os.SameFile", err)
	}
}

func TestLoad_ContextsEntryErrorNamesEntry(t *testing.T) {
	_, err := loadYAML(t, "contexts:\n  work:\n    agent: claude\n    config_dir: ~/x\n")
	if err == nil || !strings.Contains(err.Error(), "contexts.work") {
		t.Fatalf("Load() error = %v, want it to name contexts.work", err)
	}
}

func TestLoad_LegacyContextsSharingNativeDirFail(t *testing.T) {
	_, err := loadYAML(t, "contexts:\n"+
		"  a:\n    agent: claude\n    profile: shared\n"+
		"  b:\n    agent: claude\n    profile: shared\n")
	if !errors.Is(err, ErrProfileDirAlias) {
		t.Fatalf("Load() error = %v, want ErrProfileDirAlias", err)
	}
	if !strings.Contains(err.Error(), "config_dirs") {
		t.Errorf("legacy guidance should use legacy key names: %v", err)
	}
}

func TestLoad_DistinctProfileDirsPass(t *testing.T) {
	_, err := loadYAML(t, "profiles:\n"+
		"  a:\n    agent: claude\n    config_dir_name: a\n"+
		"  b:\n    agent: claude\n    config_dir_name: b\n"+
		"  c:\n    agent: claude\n")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
}

func TestLoad_ProjectConfigMayReferenceProfile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	configDir, projectDir := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(configDir, "config.yaml"), "profiles:\n  work:\n    agent: claude\n")
	writeFile(t, filepath.Join(projectDir, ProjectConfigFileName), "profile: work\n")
	cfg, err := Load(configDir, projectDir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.ProjectOverride.Profile != "work" {
		t.Errorf("ProjectOverride.Profile = %q, want work", cfg.ProjectOverride.Profile)
	}
}

// An unknown profile named in .aide.yaml loads without complaint: the
// reference never selects a profile, and the launch notice (see
// launcher.ProjectProfileNotice) explains it once, after resolution.
func TestLoad_ProjectConfigUnknownProfileLoads(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	configDir, projectDir := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(configDir, "config.yaml"), "profiles:\n  work:\n    agent: claude\n")
	writeFile(t, filepath.Join(projectDir, ProjectConfigFileName), "profile: nope\n")
	cfg, err := Load(configDir, projectDir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.ProjectOverride.Profile != "nope" {
		t.Errorf("ProjectOverride.Profile = %q, want nope", cfg.ProjectOverride.Profile)
	}
}

func TestLoad_ProjectConfigDefiningProfileFails(t *testing.T) {
	bodies := map[string]string{
		"profiles:":            "profiles:\n  work:\n    agent: claude\n",
		"contexts:":            "contexts:\n  work:\n    agent: claude\n",
		"default_profile:":     "default_profile: work\n",
		"config_dir_name:":     "config_dir_name: acme\n",
		"config_dir:":          "config_dir: ~/.claude-acme\n",
		"config_dirs:":         "config_dirs: {claude: ~/.claude-acme}\n",
		"profile_dir:":         "profile_dir: ~/.claude-acme\n",
		"match:":               "match:\n  - path: ~/src\n",
		"credentials:":         "credentials:\n  openrouter: {}\n",
		"default_credentials:": "default_credentials: [openrouter]\n",
		"secret:":              "secret: personal\n",
	}
	for key, body := range bodies {
		t.Run(key, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			configDir, projectDir := t.TempDir(), t.TempDir()
			writeFile(t, filepath.Join(configDir, "config.yaml"), "profiles:\n  work:\n    agent: claude\n")
			writeFile(t, filepath.Join(projectDir, ProjectConfigFileName), body)
			_, err := Load(configDir, projectDir)
			if !errors.Is(err, ErrProfileNotProjectScoped) {
				t.Fatalf("Load() error = %v, want ErrProfileNotProjectScoped", err)
			}
			if !strings.Contains(err.Error(), key) {
				t.Errorf("error %q does not name %s", err, key)
			}
			if !strings.Contains(err.Error(), "in your profile") {
				t.Errorf("error %q does not point at the profile", err)
			}
		})
	}
}

// A worktree-local .aide.yaml gets the same check: it may not pick
// the profile's secret store either.
func TestLoad_WorktreeFileMayNotSetSecret(t *testing.T) {
	_, wtDir := worktreeFixture(t)
	writeFile(t, filepath.Join(wtDir, ProjectConfigFileName), "secret: personal\n")
	_, err := Load(writeGlobal(t), wtDir)
	if !errors.Is(err, ErrProfileNotProjectScoped) {
		t.Fatalf("err = %v, want ErrProfileNotProjectScoped", err)
	}
}

func mkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
}

func symlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

// On a case-insensitive filesystem (macOS and Windows defaults), config
// dirs that differ only by case are one dir. Before either exists there
// is no inode to compare, so names are compared case-folded there.
func TestLoad_CaseOnlyDirDifferenceIsAliasOnCaseInsensitiveFS(t *testing.T) {
	cfg := "profiles:\n" +
		"  a:\n    agent: claude\n    config_dir_name: work\n" +
		"  b:\n    agent: claude\n    config_dir_name: Work\n"
	for _, tc := range []struct {
		fold bool
		want error
	}{{true, ErrProfileDirAlias}, {false, nil}} {
		t.Run(fmt.Sprintf("case-insensitive=%v", tc.fold), func(t *testing.T) {
			prev := caseInsensitiveDirs
			caseInsensitiveDirs = tc.fold
			t.Cleanup(func() { caseInsensitiveDirs = prev })
			if _, err := loadYAML(t, cfg); !errors.Is(err, tc.want) {
				t.Errorf("Load() error = %v, want %v", err, tc.want)
			}
		})
	}
}

// A profile pinned to a harness's global dir shares that login with a
// profile that selects no config dir and so runs from the same dir.
func TestLoad_PinnedGlobalDirAliasesUnisolatedProfileFails(t *testing.T) {
	for name, pin := range map[string]string{
		"config_dirs": "    config_dir_name: a\n    config_dirs: {claude: ~/.claude}\n",
		"config_dir":  "    config_dir_name: a\n    config_dir: ~/.claude\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := loadYAML(t, "profiles:\n"+
				"  a:\n    agent: claude\n"+pin+
				"  b:\n    agent: codex\n")
			if !errors.Is(err, ErrProfileDirAlias) {
				t.Fatalf("Load() error = %v, want ErrProfileDirAlias", err)
			}
			for _, want := range []string{`"a"`, `"b"`, "~/.claude", "config_dir_name"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not mention %s", err, want)
				}
			}
		})
	}
}

// Profiles that all select no config dir load (they never claimed
// isolation); the shared global login is refused per launch instead.
func TestGlobalLoginError(t *testing.T) {
	home := t.TempDir()
	cfg, err := loadYAML(t, "contexts:\n"+
		"  work:\n    agent: claude\n"+
		"  personal:\n    agent: claude\n"+
		"  redirected:\n    agent: claude\n    env: {CLAUDE_CONFIG_DIR: ~/.claude-r}\n"+
		"  own:\n    agent: claude\n    profile: own\n")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	err = cfg.GlobalLoginError("work", "claude", home)
	if !errors.Is(err, ErrProfileDirAlias) {
		t.Fatalf("work: err = %v, want ErrProfileDirAlias", err)
	}
	for _, want := range []string{`"personal"`, `"work"`, "~/.claude", "contexts.work: add `profile: work`"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %s", err, want)
		}
	}
	for _, name := range []string{"redirected", "own"} {
		if err := cfg.GlobalLoginError(name, "claude", home); err != nil {
			t.Errorf("%s: err = %v, want nil", name, err)
		}
	}
	if err := cfg.GlobalLoginError("work", "aider", home); err != nil {
		t.Errorf("harness without profile support: err = %v, want nil", err)
	}

	single, err := loadYAML(t, "profiles:\n"+
		"  work:\n    agent: claude\n"+
		"  personal:\n    agent: claude\n    config_dir_name: personal\n")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if err := single.GlobalLoginError("work", "claude", home); err != nil {
		t.Errorf("one profile on the global login: err = %v, want nil", err)
	}
}

// A profile whose own agent cannot select a dir does not count against
// others, but launching it under a profile-capable harness does.
func TestGlobalLoginError_AgentWithoutProfileSupport(t *testing.T) {
	home := t.TempDir()
	cfg, err := loadYAML(t, "profiles:\n  work: {agent: claude}\n  scratch: {agent: aider}\ndefault_profile: work\n")
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.GlobalLoginError("work", "claude", home); err != nil {
		t.Errorf("work: err = %v, want nil", err)
	}
	err = cfg.GlobalLoginError("scratch", "claude", home)
	// Its agent cannot take config_dir_name:, so the remedy is env: or
	// no --agent.
	if !errors.Is(err, ErrProfileDirAlias) || !strings.Contains(err.Error(), "launch it without --agent claude") ||
		!strings.Contains(err.Error(), "CLAUDE_CONFIG_DIR: ~/.claude-scratch") || strings.Contains(err.Error(), "config_dir_name: scratch") {
		t.Errorf("scratch under --agent claude: err = %v", err)
	}
}

// Profiles whose agents cannot select a dir still may not share a
// global login through --agent: with each other, or with a profile
// pinned to that dir.
func TestGlobalLoginError_AgentOverrideSharing(t *testing.T) {
	home := t.TempDir()
	cfg, err := loadYAML(t, "profiles:\n  a: {agent: aider}\n  b: {agent: aider}\n")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a", "b"} {
		if err := cfg.GlobalLoginError(name, "claude", home); !errors.Is(err, ErrProfileDirAlias) {
			t.Errorf("%s --agent claude: err = %v, want ErrProfileDirAlias", name, err)
		}
	}
	if err := cfg.GlobalLoginError("a", "aider", home); err != nil {
		t.Errorf("a on its own agent: err = %v", err)
	}

	cfg, err = loadYAML(t, "profiles:\n"+
		"  p:\n    agent: claude\n    config_dir_name: p\n    config_dirs: {claude: ~/.claude}\n"+
		"  s: {agent: aider}\n")
	if err != nil {
		t.Fatal(err)
	}
	home, _ = os.UserHomeDir() // loadYAML's HOME, which the pin expands against
	err = cfg.GlobalLoginError("s", "claude", home)
	if !errors.Is(err, ErrProfileDirAlias) || !strings.Contains(err.Error(), `To keep "p"`) {
		t.Errorf("s --agent claude beside p's pin: err = %v", err)
	}
	if err := cfg.GlobalLoginError("p", "claude", home); err != nil {
		t.Errorf("p on its own agent: err = %v (s only shares under --agent)", err)
	}
}

// An env: dir variable set to the default dir is not isolation.
func TestGlobalLoginError_EnvPointingAtDefaultDir(t *testing.T) {
	cfg, err := loadYAML(t, "profiles:\n"+
		"  a: {agent: claude, env: {CLAUDE_CONFIG_DIR: ~/.claude}}\n"+
		"  b: {agent: claude}\n")
	if err != nil {
		t.Fatal(err)
	}
	home, _ := os.UserHomeDir()
	if err := cfg.GlobalLoginError("b", "claude", home); !errors.Is(err, ErrProfileDirAlias) {
		t.Errorf("err = %v, want ErrProfileDirAlias", err)
	}
}

// A new profile gets its own dir only when another profile already
// runs its harness from the global dir.
func TestIsolateNewProfile(t *testing.T) {
	cfg := &Config{ProfileSchema: true, Contexts: map[string]Context{}}
	first := Context{Agent: "claude"}
	if others := cfg.IsolateNewProfile("work", &first); others != nil || first.Profile != "" {
		t.Errorf("first profile: others=%v profile=%q, want it left on the global dir", others, first.Profile)
	}
	cfg.Contexts["work"] = first
	second := Context{Agent: "codex"}
	if others := cfg.IsolateNewProfile("my.repo", &second); len(others) != 1 || others[0] != "work" || second.Profile != "my-repo" {
		t.Errorf("second profile: others=%v profile=%q, want [work] and my-repo", others, second.Profile)
	}
	cfg.Contexts["my.repo"] = second
	if err := cfg.GlobalLoginError("work", "codex", t.TempDir()); err != nil {
		t.Errorf("after isolating the new profile: %v", err)
	}
	// A derived name another profile already uses gets a suffix.
	cfg.Contexts["my-repo"] = Context{Agent: "claude", Profile: "my-repo-2"}
	third := Context{Agent: "claude"}
	if cfg.IsolateNewProfile("my_repo", &third); third.Profile != "my_repo" {
		t.Errorf("valid name changed: %q", third.Profile)
	}
	fourth := Context{Agent: "claude"}
	if cfg.IsolateNewProfile("my repo", &fourth); fourth.Profile != "my-repo-3" {
		t.Errorf("colliding name = %q, want my-repo-3", fourth.Profile)
	}
	tool := Context{Agent: "aider"}
	if cfg.IsolateNewProfile("tool", &tool); tool.Profile != "" {
		t.Errorf("agent without profile support got config_dir_name %q", tool.Profile)
	}
}
