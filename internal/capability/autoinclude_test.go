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
