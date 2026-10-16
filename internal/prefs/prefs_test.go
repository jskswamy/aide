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
	f.Set(key, Preference{Harness: "pi", Credentials: Selection("openrouter")})
	if err := Save(path, f); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	again, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	got, ok := again.Get(key)
	if !ok || got.Harness != "pi" || got.Credentials == nil || strings.Join(*got.Credentials, ",") != "openrouter" {
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
	f.Set(key, Preference{Credentials: Selection()})
	if err := Save(path, f); err != nil {
		t.Fatal(err)
	}
	again, _ := Load(path)
	got, ok := again.Get(key)
	if !ok {
		t.Fatal("explicit empty selection was dropped")
	}
	if got.Credentials == nil || len(*got.Credentials) != 0 {
		t.Errorf("Credentials = %v, want explicit empty selection", got.Credentials)
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
	if !strings.Contains(string(data), `"version": 1`) {
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
