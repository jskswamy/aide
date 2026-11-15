package prefs

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoad_MissingFileIsEmpty(t *testing.T) {
	f, err := Load(filepath.Join(t.TempDir(), "none.json"))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if _, ok := f.Get(Key{Profile: "work", Remote: "github.com/o/r"}); ok {
		t.Error("empty file reported a preference")
	}
}

func TestSaveLoad_RoundTripSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "preferences.json")
	key := Key{Profile: "work", Remote: "github.com/o/r"}
	f, _ := Load(path)
	f.Set(key, Preference{Harness: "pi", LegacyCredentials: Selection("openrouter")})
	if err := Save(path, f); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	again, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	got, ok := again.Get(key)
	if !ok || got.Harness != "pi" || got.LegacyCredentials == nil || strings.Join(*got.LegacyCredentials, ",") != "openrouter" {
		t.Errorf("Get() = %+v, %v; want harness pi, credentials [openrouter]", got, ok)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("file mode = %v, want 0600", info.Mode().Perm())
	}
}

func TestPreference_EmptySelectionDistinctFromUnset(t *testing.T) {
	path := filepath.Join(t.TempDir(), "preferences.json")
	key := Key{Profile: "work", Remote: "github.com/o/r"}
	f, _ := Load(path)
	f.Set(key, Preference{LegacyCredentials: Selection()})
	if err := Save(path, f); err != nil {
		t.Fatal(err)
	}
	again, _ := Load(path)
	got, ok := again.Get(key)
	if !ok {
		t.Fatal("explicit empty selection was dropped")
	}
	if got.LegacyCredentials == nil || len(*got.LegacyCredentials) != 0 {
		t.Errorf("Credentials = %v, want explicit empty selection", got.LegacyCredentials)
	}
}

func TestSet_ZeroPreferenceRemovesEntry(t *testing.T) {
	key := Key{Profile: "work", Remote: "github.com/o/r"}
	f := &File{}
	f.Set(key, Preference{Harness: "pi"})
	f.Set(key, Preference{})
	if _, ok := f.Get(key); ok {
		t.Error("cleared preference still present")
	}
	if len(f.Profiles) != 0 {
		t.Errorf("empty profile map left behind: %v", f.Profiles)
	}
}

func TestGet_ScopedByProfileAndRemote(t *testing.T) {
	f := &File{}
	f.Set(Key{Profile: "work", Remote: "github.com/o/r"}, Preference{Harness: "pi"})
	if _, ok := f.Get(Key{Profile: "personal", Remote: "github.com/o/r"}); ok {
		t.Error("preference crossed profiles")
	}
	if _, ok := f.Get(Key{Profile: "work", Remote: "github.com/o/other"}); ok {
		t.Error("preference crossed remotes")
	}
}

func TestLoad_FutureVersionFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "preferences.json")
	if err := os.WriteFile(path, []byte(`{"version": 99, "profiles": {}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if !errors.Is(err, ErrUnsupportedVersion) {
		t.Fatalf("Load() error = %v, want ErrUnsupportedVersion", err)
	}
}

func TestLoad_CorruptFileFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "preferences.json")
	if err := os.WriteFile(path, []byte(`{not json`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("Load() of a corrupt file should fail")
	}
}

func TestUpdate_RefusesToOverwriteFutureVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "preferences.json")
	future := []byte(`{"version": 99, "profiles": {}}`)
	if err := os.WriteFile(path, future, 0o600); err != nil {
		t.Fatal(err)
	}
	err := Update(path, func(f *File) error {
		f.Set(Key{Profile: "work", Remote: "github.com/o/r"}, Preference{Harness: "pi"})
		return nil
	})
	if !errors.Is(err, ErrUnsupportedVersion) {
		t.Fatalf("Update() error = %v, want ErrUnsupportedVersion", err)
	}
	if got, _ := os.ReadFile(path); string(got) != string(future) {
		t.Error("Update() overwrote a newer file")
	}
}

func TestUpdate_WritesVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "preferences.json")
	if err := Update(path, func(f *File) error {
		f.Set(Key{Profile: "work", Remote: "github.com/o/r"}, Preference{Harness: "pi"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), `"version": 2`) {
		t.Errorf("file lacks version:\n%s", data)
	}
}

func TestDefaultPath_UnderStateDir(t *testing.T) {
	if got := DefaultPath("/home/u"); got != "/home/u/.local/state/aide/preferences.json" {
		t.Errorf("DefaultPath() = %q", got)
	}
}

func TestKey_RequiresBothParts(t *testing.T) {
	if err := (Key{Profile: "work"}).Validate(); !errors.Is(err, ErrNoRepositoryIdentity) {
		t.Errorf("Validate() without remote = %v, want ErrNoRepositoryIdentity", err)
	}
	if err := (Key{Remote: "github.com/o/r"}).Validate(); err == nil {
		t.Error("Validate() without profile should fail")
	}
}

func TestLoad_CorruptFileNamesFileAndRemedy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "preferences.json")
	for _, body := range []string{`{not json`, `{}`, `[1,2]`} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := Load(path)
		if !errors.Is(err, ErrCorrupt) {
			t.Fatalf("Load(%s) error = %v, want ErrCorrupt", body, err)
		}
		if !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "aide project clear --reset") {
			t.Errorf("error should name the file and the remedy: %v", err)
		}
	}
}

func TestReset_MovesFileAside(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "preferences.json")
	if err := os.WriteFile(path, []byte(`{not json`), 0o600); err != nil {
		t.Fatal(err)
	}
	moved, err := Reset(path)
	if err != nil {
		t.Fatalf("Reset() error = %v", err)
	}
	if !strings.HasPrefix(moved, path+".corrupt-") {
		t.Errorf("moved to %q", moved)
	}
	if got, _ := os.ReadFile(moved); string(got) != `{not json` {
		t.Error("moved file lost its content")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("original file still present")
	}
	if f, err := Load(path); err != nil || len(f.Profiles) != 0 {
		t.Errorf("after Reset, Load() = %v, %v; want empty", f, err)
	}
	if moved, err := Reset(path); err != nil || moved != "" {
		t.Errorf("Reset() with no file = %q, %v; want nothing moved", moved, err)
	}
}

func TestUpdate_UnchangedWritesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "preferences.json")
	if err := Update(path, func(*File) error { return ErrUnchanged }); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("Update() wrote the file although nothing changed")
	}
}

func TestLoad_Version1KeepsCredentialsAndSavesVersion2(t *testing.T) {
	path := filepath.Join(t.TempDir(), "preferences.json")
	v1 := `{"version": 1, "profiles": {"work": {"github.com/o/r": {"harness": "pi", "credentials": ["corp"]}}}}`
	if err := os.WriteFile(path, []byte(v1), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	key := Key{Profile: "work", Remote: "github.com/o/r"}
	p, _ := f.Get(key)
	if p.LegacyCredentials == nil || (*p.LegacyCredentials)[0] != "corp" || len(p.Gateway) != 0 {
		t.Fatalf("v1 record = %+v, want credentials [corp] and no gateway choice", p)
	}
	p.Gateway = map[string]string{"codex": "none"}
	f.Set(key, p)
	if err := Save(path, f); err != nil {
		t.Fatal(err)
	}
	again, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := again.Get(key)
	if again.Version != 2 || got.Gateway["codex"] != "none" || got.LegacyCredentials == nil {
		t.Errorf("after Save: version %d, %+v; want v2 with gateway and untranslated credentials", again.Version, got)
	}
}

func TestPreference_GatewayChoiceIsNotZero(t *testing.T) {
	if (Preference{Gateway: map[string]string{"claude": "none"}}).IsZero() {
		t.Error("a preference with a gateway choice must not be zero")
	}
}

func TestSaveBacksUpVersion1FileOnce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "preferences.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"profiles":{"work":{"github.com/a/b":{"credentials":[]}}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(dir, "backup")
	if err := SaveBackingUpTo(path, f, root); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(root, "preferences.json.bak"))
	if err != nil || !strings.Contains(string(b), `"version":1`) {
		t.Fatalf("backup = %q, %v", b, err)
	}
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	if err := SaveBackingUpTo(path, f, root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(root); err == nil {
		t.Fatal("a version 2 file must not be backed up again")
	}
}
