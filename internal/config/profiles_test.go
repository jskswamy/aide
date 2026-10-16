package config

import (
	"bytes"
	"errors"
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

func TestLoad_ProjectConfigUnknownProfileWarnsOnce(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var warned bytes.Buffer
	prev := warnOut
	warnOut = &warned
	t.Cleanup(func() { warnOut = prev })
	configDir, projectDir := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(configDir, "config.yaml"), "profiles:\n  work:\n    agent: claude\n")
	writeFile(t, filepath.Join(projectDir, ProjectConfigFileName), "profile: nope\n")
	for range 3 {
		if _, err := Load(configDir, projectDir); err != nil {
			t.Fatal(err)
		}
	}
	if n := strings.Count(warned.String(), "warning"); n != 1 {
		t.Errorf("warned %d times, want once:\n%s", n, warned.String())
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

func TestLoad_ProjectConfigUnknownProfileWarns(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var warned bytes.Buffer
	prev := warnOut
	warnOut = &warned
	t.Cleanup(func() { warnOut = prev })
	configDir, projectDir := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(configDir, "config.yaml"), "profiles:\n  work:\n    agent: claude\n")
	writeFile(t, filepath.Join(projectDir, ProjectConfigFileName), "profile: nope\n")
	cfg, err := Load(configDir, projectDir)
	if err != nil {
		t.Fatalf("Load() error = %v, want a warning only", err)
	}
	if cfg.ProjectOverride.Profile != "nope" {
		t.Errorf("ProjectOverride.Profile = %q, want nope", cfg.ProjectOverride.Profile)
	}
	for _, want := range []string{ProjectConfigFileName, `"nope"`, "work", "profiles:"} {
		if !strings.Contains(warned.String(), want) {
			t.Errorf("warning %q does not mention %s", warned.String(), want)
		}
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
		})
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
