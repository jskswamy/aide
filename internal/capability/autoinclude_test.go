package capability

import (
	"os"
	"path/filepath"
	"testing"
)

func writeCcstatuslineSettings(t *testing.T, homeDir string) {
	t.Helper()
	dir := filepath.Join(homeDir, ".config", "ccstatusline")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAutoInclude_CcstatuslineAddsWhenSettingsFileExists(t *testing.T) {
	homeDir := t.TempDir()
	writeCcstatuslineSettings(t, homeDir)
	got := AutoInclude([]string{"k8s"}, nil, homeDir, noEnv)
	if len(got) != 2 || got[0] != "k8s" || got[1] != "ccstatusline" {
		t.Errorf("got %v, want [k8s ccstatusline]", got)
	}
}

func TestAutoInclude_CcstatuslineNoOpWhenSettingsFileMissing(t *testing.T) {
	homeDir := t.TempDir()
	got := AutoInclude([]string{"k8s"}, nil, homeDir, noEnv)
	if len(got) != 1 || got[0] != "k8s" {
		t.Errorf("got %v, want [k8s]", got)
	}
}

func TestAutoInclude_CcstatuslineNoOpWhenAlreadyPresent(t *testing.T) {
	homeDir := t.TempDir()
	writeCcstatuslineSettings(t, homeDir)
	got := AutoInclude([]string{"ccstatusline"}, nil, homeDir, noEnv)
	if len(got) != 1 || got[0] != "ccstatusline" {
		t.Errorf("got %v, want [ccstatusline] (not duplicated)", got)
	}
}

func TestAutoInclude_CcstatuslineRespectsExplicitExclusion(t *testing.T) {
	homeDir := t.TempDir()
	writeCcstatuslineSettings(t, homeDir)
	got := AutoInclude([]string{"k8s"}, []string{"ccstatusline"}, homeDir, noEnv)
	if len(got) != 1 || got[0] != "k8s" {
		t.Errorf("got %v, want [k8s] (explicit --without honored)", got)
	}
}

func noEnv(string) string { return "" }

func TestAutoInclude_SkillshareWhenConfigDirExists(t *testing.T) {
	homeDir := t.TempDir()
	if got := AutoInclude([]string{"k8s"}, nil, homeDir, noEnv); len(got) != 1 {
		t.Errorf("got %v, want [k8s] without Skillshare", got)
	}
	if err := os.MkdirAll(filepath.Join(homeDir, ".config", "skillshare"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := AutoInclude([]string{"k8s"}, nil, homeDir, noEnv)
	if len(got) != 2 || got[1] != "skillshare" {
		t.Errorf("got %v, want [k8s skillshare]", got)
	}
	if got := AutoInclude([]string{"skillshare"}, nil, homeDir, noEnv); len(got) != 1 {
		t.Errorf("got %v, want [skillshare] (not duplicated)", got)
	}
	if got := AutoInclude([]string{"k8s"}, []string{"skillshare"}, homeDir, noEnv); len(got) != 1 {
		t.Errorf("got %v, want [k8s] (exclusion honored)", got)
	}
}

func TestAutoInclude_SkillshareFollowsXDGConfigHome(t *testing.T) {
	homeDir, xdg := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(xdg, "skillshare"), 0o755); err != nil {
		t.Fatal(err)
	}
	env := func(k string) string {
		if k == "XDG_CONFIG_HOME" {
			return xdg
		}
		return ""
	}
	if got := AutoInclude(nil, nil, homeDir, env); len(got) != 1 || got[0] != "skillshare" {
		t.Errorf("got %v, want [skillshare]", got)
	}
}

func TestAutoInclude_BivouacWhenConfigDirExists(t *testing.T) {
	homeDir := t.TempDir()
	if got := AutoInclude([]string{"k8s"}, nil, homeDir, noEnv); len(got) != 1 {
		t.Errorf("got %v, want [k8s] without bivouac", got)
	}
	if err := os.MkdirAll(filepath.Join(homeDir, ".config", "bivouac"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := AutoInclude([]string{"k8s"}, nil, homeDir, noEnv); len(got) != 2 || got[1] != "bivouac" {
		t.Errorf("got %v, want [k8s bivouac]", got)
	}
	if got := AutoInclude([]string{"bivouac"}, nil, homeDir, noEnv); len(got) != 1 {
		t.Errorf("got %v, want [bivouac] (not duplicated)", got)
	}
	if got := AutoInclude([]string{"k8s"}, []string{"bivouac"}, homeDir, noEnv); len(got) != 1 {
		t.Errorf("got %v, want [k8s] (exclusion honored)", got)
	}
}

func TestAutoInclude_PonytailWhenInstalled(t *testing.T) {
	for name, dir := range map[string]string{
		"config dir": ".config/ponytail",
		"skillshare": ".local/state/skillshare/plugins/abc/skillshare-ponytail-123",
	} {
		homeDir := t.TempDir()
		if got := AutoInclude([]string{"k8s"}, nil, homeDir, noEnv); len(got) != 1 {
			t.Errorf("%s: got %v, want [k8s] without ponytail", name, got)
		}
		if err := os.MkdirAll(filepath.Join(homeDir, dir), 0o755); err != nil {
			t.Fatal(err)
		}
		if got := AutoInclude([]string{"k8s"}, nil, homeDir, noEnv); len(got) != 2 || got[1] != "ponytail" {
			t.Errorf("%s: got %v, want [k8s ponytail]", name, got)
		}
		if got := AutoInclude([]string{"k8s"}, []string{"ponytail"}, homeDir, noEnv); len(got) != 1 {
			t.Errorf("%s: got %v, want [k8s] (exclusion honored)", name, got)
		}
	}
}

func TestAutoInclude_TerminalBrowserWhenHashedDirExists(t *testing.T) {
	for name, rel := range map[string]string{
		"release data dir":  ".local/share/terminal-browser-58a67c5b",
		"dev state dir":     ".local/state/terminal-browser-dev-abcdef01",
		"release state dir": ".local/state/terminal-browser-01234567",
	} {
		t.Run(name, func(t *testing.T) {
			homeDir := t.TempDir()
			if got := AutoInclude([]string{"k8s"}, nil, homeDir, noEnv); len(got) != 1 {
				t.Errorf("got %v, want [k8s] without terminal-browser", got)
			}
			if err := os.MkdirAll(filepath.Join(homeDir, rel), 0o755); err != nil {
				t.Fatal(err)
			}
			if got := AutoInclude([]string{"k8s"}, nil, homeDir, noEnv); len(got) != 2 || got[1] != "terminal-browser" {
				t.Errorf("got %v, want [k8s terminal-browser]", got)
			}
			if got := AutoInclude([]string{"terminal-browser"}, nil, homeDir, noEnv); len(got) != 1 {
				t.Errorf("got %v, want [terminal-browser] (not duplicated)", got)
			}
			if got := AutoInclude([]string{"k8s"}, []string{"terminal-browser"}, homeDir, noEnv); len(got) != 1 {
				t.Errorf("got %v, want [k8s] (exclusion honored)", got)
			}
		})
	}
}

func TestAutoInclude_TerminalBrowserIgnoresNonMatchingAndFiles(t *testing.T) {
	homeDir := t.TempDir()
	// Non-matching sibling dirs and a regular file with a matching name must not trigger auto-include.
	for _, dir := range []string{
		".local/share/terminal-browser-interop",
		".local/share/terminal-browser-short",
		".local/share/terminal-browser-ZZZZZZZZ",
		".local/share/other-app",
	} {
		if err := os.MkdirAll(filepath.Join(homeDir, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(homeDir, ".local", "share", "terminal-browser-58a67c5b"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := AutoInclude([]string{"k8s"}, nil, homeDir, noEnv); len(got) != 1 || got[0] != "k8s" {
		t.Errorf("got %v, want [k8s]", got)
	}
}

func TestAutoInclude_TerminalBrowserFollowsXDGAndSymlinks(t *testing.T) {
	homeDir := t.TempDir()
	rtDir := t.TempDir()
	target := filepath.Join(t.TempDir(), "tb-real")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(rtDir, "terminal-browser-deadbeef")); err != nil {
		t.Fatal(err)
	}
	env := func(k string) string {
		if k == "XDG_RUNTIME_DIR" {
			return rtDir
		}
		return ""
	}
	if got := AutoInclude(nil, nil, homeDir, env); len(got) != 1 || got[0] != "terminal-browser" {
		t.Errorf("got %v, want [terminal-browser]", got)
	}
}
