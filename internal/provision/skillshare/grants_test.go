package skillshare

import (
	"github.com/jskswamy/aide/internal/testutil"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/jskswamy/aide/internal/config"
)

// skillshareHome lays out a home with every Skillshare dir: the ones
// the capability grants and the ones it must not.
func skillshareHome(t *testing.T) string {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{
		".config/skillshare/skills", ".config/skillshare/agents",
		".local/state/skillshare/plugins", ".local/state/skillshare/hooks",
		".local/state/skillshare/mcp", ".local/state/skillshare/logs",
		".local/share/skillshare/backups", ".cache/skillshare",
	} {
		mkdir(t, filepath.Join(home, d))
	}
	writeFile(t, filepath.Join(home, ".config/skillshare/config.yaml"), "targets: {}\n")
	return home
}

func mkdir(t *testing.T, d string) {
	t.Helper()
	if err := os.MkdirAll(d, 0o700); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, p, content string) {
	t.Helper()
	mkdir(t, filepath.Dir(p))
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func envOf(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestInstalled(t *testing.T) {
	home := t.TempDir()
	if Installed(home, envOf(nil)) {
		t.Error("Installed without a Skillshare config dir")
	}
	mkdir(t, filepath.Join(home, ".config", "skillshare"))
	if !Installed(home, envOf(nil)) {
		t.Error("not Installed with ~/.config/skillshare")
	}
	xdg := t.TempDir()
	if Installed(home, envOf(map[string]string{"XDG_CONFIG_HOME": xdg})) {
		t.Error("Installed looked past $XDG_CONFIG_HOME")
	}
	mkdir(t, filepath.Join(xdg, "skillshare"))
	if !Installed(home, envOf(map[string]string{"XDG_CONFIG_HOME": xdg})) {
		t.Error("not Installed with $XDG_CONFIG_HOME/skillshare")
	}
}

func TestReadableDirs_GrantsOnlySkillsAgentsAndPlugins(t *testing.T) {
	home := skillshareHome(t)

	got := ReadableDirs(&config.Config{}, home, envOf(nil))

	want := []string{
		filepath.Join(home, ".config/skillshare/skills"),
		filepath.Join(home, ".config/skillshare/agents"),
		filepath.Join(home, ".local/state/skillshare/plugins"),
	}
	if !slices.Equal(got, want) {
		t.Errorf("ReadableDirs = %v, want %v", got, want)
	}
}

func TestReadableDirs_SkipsMissingDirs(t *testing.T) {
	home := testutil.CanonicalTempDir(t)
	mkdir(t, filepath.Join(home, ".config/skillshare/skills"))

	got := ReadableDirs(&config.Config{}, home, envOf(nil))

	if len(got) != 1 || filepath.Base(got[0]) != "skills" {
		t.Errorf("ReadableDirs = %v, want only skills/", got)
	}
}

func TestReadableDirs_FollowsXDGDirs(t *testing.T) {
	home := t.TempDir()
	cfgHome, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	stateHome, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{
		filepath.Join(cfgHome, "skillshare/skills"), filepath.Join(stateHome, "skillshare/plugins"),
		filepath.Join(home, ".config/skillshare/agents"),
	} {
		mkdir(t, d)
	}

	got := ReadableDirs(&config.Config{}, home, envOf(map[string]string{"XDG_CONFIG_HOME": cfgHome, "XDG_STATE_HOME": stateHome}))

	want := []string{filepath.Join(cfgHome, "skillshare/skills"), filepath.Join(stateHome, "skillshare/plugins")}
	if !slices.Equal(got, want) {
		t.Errorf("ReadableDirs = %v, want %v", got, want)
	}
}

func TestReadableDirs_ListsResolvedAndLiteralPaths(t *testing.T) {
	home := skillshareHome(t)
	target := filepath.Join(home, "dotfiles", "skills")
	mkdir(t, target)
	link := filepath.Join(home, ".config/skillshare/skills")
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	got := ReadableDirs(&config.Config{}, home, envOf(nil))

	if !slices.Contains(got, target) || !slices.Contains(got, link) {
		t.Errorf("ReadableDirs = %v, want both %s and %s", got, target, link)
	}
}

func TestReadableDirs_GrantsSafeSources(t *testing.T) {
	home := skillshareHome(t)
	mine := filepath.Join(home, "src", "my-skills")
	viaLink := filepath.Join(home, "src", "team-skills")
	mkdir(t, mine)
	mkdir(t, viaLink)
	if err := os.Symlink(viaLink, filepath.Join(home, "team")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(home, ".config/skillshare/config.yaml"),
		"source: ~/src/my-skills\nsources:\n  skills: ~/team\n  extra: [~/src/missing]\n")

	got := ReadableDirs(&config.Config{}, home, envOf(nil))

	for _, p := range []string{mine, viaLink, filepath.Join(home, "team")} {
		if !slices.Contains(got, p) {
			t.Errorf("ReadableDirs = %v, missing %s", got, p)
		}
	}
	if slices.Contains(got, filepath.Join(home, "src", "missing")) {
		t.Errorf("ReadableDirs listed a missing source: %v", got)
	}
}

func TestReadableDirs_UsesSkillshareConfigOverride(t *testing.T) {
	home := skillshareHome(t)
	mine := filepath.Join(home, "src", "my-skills")
	mkdir(t, mine)
	writeFile(t, filepath.Join(home, "ss.yaml"), "sources:\n  skills: ~/src/my-skills\n")

	got := ReadableDirs(&config.Config{}, home, envOf(map[string]string{"SKILLSHARE_CONFIG": "~/ss.yaml"}))

	if !slices.Contains(got, mine) {
		t.Errorf("ReadableDirs = %v, missing %s from $SKILLSHARE_CONFIG", got, mine)
	}
}

func TestReadableDirs_RefusesUnsafeSources(t *testing.T) {
	outside, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for name, src := range map[string]string{
		"home":             "~",
		"root":             "/",
		"relative":         "src/skills",
		"outside home":     outside,
		"sensitive":        "~/.ssh",
		"inside sensitive": "~/.aws/skills",
		"contains config":  "~/.config",
		"aide config":      "~/.config/aide",
		"harness default":  "~/.claude",
		"default subdir":   "~/.codex/skills",
		"other profile":    "~/.claude-work",
		"link to profile":  "~/innocent",
		"template":         "~/{{ .home }}",
		"link to template": "~/tmpl",
	} {
		t.Run(name, func(t *testing.T) {
			home := skillshareHome(t)
			for _, d := range []string{".ssh", ".aws/skills", ".config/aide", ".claude", ".codex/skills", ".claude-work", "{{ .home }}"} {
				mkdir(t, filepath.Join(home, d))
			}
			if err := os.Symlink(filepath.Join(home, ".claude-work"), filepath.Join(home, "innocent")); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join(home, "{{ .home }}"), filepath.Join(home, "tmpl")); err != nil {
				t.Fatal(err)
			}
			mkdir(t, filepath.Join(home, "src", "skills"))
			writeFile(t, filepath.Join(home, ".config/skillshare/config.yaml"), "sources:\n  skills: "+src+"\n")
			cfg := &config.Config{Contexts: map[string]config.Context{"work": {Agent: "claude", Profile: "work"}}}

			got := ReadableDirs(cfg, home, envOf(nil))

			if len(got) != 3 {
				t.Errorf("ReadableDirs = %v, want only the three fixed dirs", got)
			}
		})
	}
}

func TestReadableDirs_RefusesFixedDirLinkedIntoProtectedDir(t *testing.T) {
	home := skillshareHome(t)
	mkdir(t, filepath.Join(home, ".claude", "skills"))
	link := filepath.Join(home, ".config/skillshare/skills")
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(home, ".claude", "skills"), link); err != nil {
		t.Fatal(err)
	}

	got := ReadableDirs(&config.Config{}, home, envOf(nil))

	for _, p := range got {
		if filepath.Base(p) == "skills" {
			t.Errorf("ReadableDirs granted %s, which resolves into ~/.claude", p)
		}
	}
}
