package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/jskswamy/aide/internal/provision"
)

func TestGrantDirectory_GlobalScope_FreshFile(t *testing.T) {
	home := t.TempDir()
	ctx := provision.Context{HomeDir: home}
	d := &Driver{}

	if err := d.GrantDirectory(ctx, "/repo/a", false); err != nil {
		t.Fatalf("GrantDirectory: %v", err)
	}

	got := readAdditionalDirectoriesFromDisk(t, filepath.Join(home, ".claude", "settings.json"))
	if len(got) != 1 || got[0] != "/repo/a" {
		t.Fatalf("additionalDirectories = %v, want [/repo/a]", got)
	}
}

func TestGrantDirectory_ProjectScope_WritesSettingsLocal(t *testing.T) {
	project := t.TempDir()
	ctx := provision.Context{HomeDir: t.TempDir(), ProjectRoot: project}
	d := &Driver{}

	if err := d.GrantDirectory(ctx, "/repo/a", false); err != nil {
		t.Fatalf("GrantDirectory: %v", err)
	}

	path := filepath.Join(project, ".claude", "settings.local.json")
	got := readAdditionalDirectoriesFromDisk(t, path)
	if len(got) != 1 || got[0] != "/repo/a" {
		t.Fatalf("additionalDirectories = %v, want [/repo/a]", got)
	}
}

func TestGrantDirectory_PreservesExistingKeys(t *testing.T) {
	home := t.TempDir()
	settingsFile := filepath.Join(home, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsFile), 0o755); err != nil {
		t.Fatal(err)
	}
	existing := `{"permissions":{"allow":["Bash(git *)"]},"model":"sonnet"}`
	if err := os.WriteFile(settingsFile, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := provision.Context{HomeDir: home}
	d := &Driver{}

	if err := d.GrantDirectory(ctx, "/repo/a", false); err != nil {
		t.Fatalf("GrantDirectory: %v", err)
	}

	raw := readRawSettings(t, settingsFile)
	if raw["model"] != "sonnet" {
		t.Errorf("model key lost: %v", raw)
	}
	perms, _ := raw["permissions"].(map[string]interface{})
	allow, _ := perms["allow"].([]interface{})
	if len(allow) != 1 || allow[0] != "Bash(git *)" {
		t.Errorf("allow list lost: %v", perms)
	}
	dirs := readAdditionalDirectoriesFromDisk(t, settingsFile)
	if len(dirs) != 1 || dirs[0] != "/repo/a" {
		t.Errorf("additionalDirectories = %v", dirs)
	}
}

func TestGrantDirectory_NoDuplicateOnRepeat(t *testing.T) {
	home := t.TempDir()
	ctx := provision.Context{HomeDir: home}
	d := &Driver{}

	mustGrant(t, d, ctx, "/repo/a")
	mustGrant(t, d, ctx, "/repo/a")

	got := readAdditionalDirectoriesFromDisk(t, filepath.Join(home, ".claude", "settings.json"))
	if len(got) != 1 {
		t.Fatalf("additionalDirectories = %v, want exactly one entry", got)
	}
}

func TestRevokeDirectory_ExactMatchRemoved(t *testing.T) {
	home := t.TempDir()
	ctx := provision.Context{HomeDir: home}
	d := &Driver{}
	mustGrant(t, d, ctx, "/repo/a")

	if err := d.RevokeDirectory(ctx, "/repo/a"); err != nil {
		t.Fatalf("RevokeDirectory: %v", err)
	}

	got := readAdditionalDirectoriesFromDisk(t, filepath.Join(home, ".claude", "settings.json"))
	if len(got) != 0 {
		t.Fatalf("additionalDirectories = %v, want empty", got)
	}
}

func TestRevokeDirectory_NestedPathRemoved(t *testing.T) {
	home := t.TempDir()
	ctx := provision.Context{HomeDir: home}
	d := &Driver{}
	mustGrant(t, d, ctx, "/repo/a/sub")

	if err := d.RevokeDirectory(ctx, "/repo/a"); err != nil {
		t.Fatalf("RevokeDirectory: %v", err)
	}

	got := readAdditionalDirectoriesFromDisk(t, filepath.Join(home, ".claude", "settings.json"))
	if len(got) != 0 {
		t.Fatalf("additionalDirectories = %v, want empty (nested path should be removed)", got)
	}
}

func TestRevokeDirectory_SiblingWithSharedPrefixUntouched(t *testing.T) {
	home := t.TempDir()
	ctx := provision.Context{HomeDir: home}
	d := &Driver{}
	mustGrant(t, d, ctx, "/repo/bar")
	mustGrant(t, d, ctx, "/repo/barbaz")

	if err := d.RevokeDirectory(ctx, "/repo/bar"); err != nil {
		t.Fatalf("RevokeDirectory: %v", err)
	}

	got := readAdditionalDirectoriesFromDisk(t, filepath.Join(home, ".claude", "settings.json"))
	if len(got) != 1 || got[0] != "/repo/barbaz" {
		t.Fatalf("additionalDirectories = %v, want [/repo/barbaz] (sibling must survive)", got)
	}
}

func TestRevokeDirectory_ParentOfDeniedPathUntouched(t *testing.T) {
	home := t.TempDir()
	ctx := provision.Context{HomeDir: home}
	d := &Driver{}
	mustGrant(t, d, ctx, "/repo")

	if err := d.RevokeDirectory(ctx, "/repo/a"); err != nil {
		t.Fatalf("RevokeDirectory: %v", err)
	}

	got := readAdditionalDirectoriesFromDisk(t, filepath.Join(home, ".claude", "settings.json"))
	if len(got) != 1 || got[0] != "/repo" {
		t.Fatalf("additionalDirectories = %v, want [/repo] (parent grant must survive)", got)
	}
}

func TestRevokeDirectory_NoopWhenFileMissing(t *testing.T) {
	home := t.TempDir()
	ctx := provision.Context{HomeDir: home}
	d := &Driver{}

	if err := d.RevokeDirectory(ctx, "/repo/a"); err != nil {
		t.Fatalf("RevokeDirectory on missing file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "settings.json")); err == nil {
		t.Fatalf("RevokeDirectory should not create a settings file when nothing changed")
	}
}

func mustGrant(t *testing.T, d *Driver, ctx provision.Context, path string) {
	t.Helper()
	if err := d.GrantDirectory(ctx, path, false); err != nil {
		t.Fatalf("setup GrantDirectory(%s): %v", path, err)
	}
}

func readRawSettings(t *testing.T, path string) map[string]interface{} {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return raw
}

func readAdditionalDirectoriesFromDisk(t *testing.T, path string) []string {
	t.Helper()
	return readAdditionalDirectories(readRawSettings(t, path))
}
