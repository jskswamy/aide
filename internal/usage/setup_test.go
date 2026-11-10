package usage_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/usage"
)

func setupHome(t *testing.T) (string, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg"))
	return home, filepath.Join(home, ".claude", "ccusage.json")
}

func setupRun(cfg *config.Config, home, answer string, force, interactive bool) (string, error) {
	var out bytes.Buffer
	err := usage.DefaultBackend().Setup(cfg, home, strings.NewReader(answer), &out, force, interactive)
	return out.String(), err
}

func storesIn(t *testing.T, path string) []struct{ Name, Path string } {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Pi struct {
			Stores []struct{ Name, Path string } `json:"stores"`
		} `json:"pi"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	return doc.Pi.Stores
}

func TestDerivePiStores_SortedSanitizedAndOmp(t *testing.T) {
	home, path := setupHome(t)
	if err := os.MkdirAll(filepath.Join(home, ".omp", "agent", "sessions"), 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Contexts: map[string]config.Context{
		"work": {Profile: "work"}, "default": {}, "Forge Team!": {Profile: "forge"},
		"LONG-abcdefghijklmnopqrstuvwxyz-remaining": {Profile: "long"},
	}}
	if _, err := setupRun(cfg, home, "", true, false); err != nil {
		t.Fatal(err)
	}
	got := storesIn(t, path)
	want := []struct{ Name, Path string }{
		{"pi-forge-team-", filepath.Join(home, ".pi-forge", "sessions")},
		{"pi-long-abcdefghijklmnopqrstuvwx", filepath.Join(home, ".pi-long", "sessions")},
		{"pi-work", filepath.Join(home, ".pi-work", "sessions")},
		{"omp", filepath.Join(home, ".omp", "agent", "sessions")},
	}
	if len(got) != len(want) {
		t.Fatalf("stores = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("store %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestDerivePiStores_OmpCoveredByProfile(t *testing.T) {
	home, path := setupHome(t)
	dir := filepath.Join(home, ".omp", "agent")
	if err := os.MkdirAll(filepath.Join(dir, "sessions"), 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Contexts: map[string]config.Context{"other": {Profile: "other", ConfigDirs: map[string]string{"pi": dir}}}}
	if _, err := setupRun(cfg, home, "", true, false); err != nil {
		t.Fatal(err)
	}
	got := storesIn(t, path)
	if len(got) != 1 || got[0].Name != "pi-other" {
		t.Errorf("stores = %+v, want only pi-other", got)
	}
}

func TestDerivePiStores_OmpContainingProfileIsNotCovered(t *testing.T) {
	home, path := setupHome(t)
	omp := filepath.Join(home, ".omp", "agent", "sessions")
	if err := os.MkdirAll(omp, 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Contexts: map[string]config.Context{"nested": {Profile: "nested", ConfigDirs: map[string]string{"pi": filepath.Join(omp, "child")}}}}
	if _, err := setupRun(cfg, home, "", true, false); err == nil || !strings.Contains(err.Error(), "overlap") {
		t.Errorf("error = %v, want overlap", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("created file on overlap: %v", err)
	}
}

func TestSetupPiStores_PreservesOtherKeysAndShowsDiff(t *testing.T) {
	home, path := setupHome(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	old := []byte(`{"claude":{"x":1},"pi":{"custom":{"enabled":true},"stores":[{"name":"old","path":"/old"}]},"future":[3]}`)
	if err := os.WriteFile(path, old, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Contexts: map[string]config.Context{"work": {Profile: "work"}}}
	out, err := setupRun(cfg, home, "y\n", false, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "--- ") || !strings.Contains(out, "+++ ") || !strings.Contains(out, "[y/N]") || !strings.Contains(out, "pi-work") {
		t.Errorf("diff/prompt missing: %q", out)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"claude", "future"} {
		var a, b any
		_ = json.Unmarshal(doc[key], &a)
		var before map[string]json.RawMessage
		_ = json.Unmarshal(old, &before)
		_ = json.Unmarshal(before[key], &b)
		if !jsonEqual(a, b) {
			t.Errorf("%s changed: %s", key, doc[key])
		}
	}
	var nested map[string]json.RawMessage
	if err := json.Unmarshal(doc["pi"], &nested); err != nil {
		t.Fatal(err)
	}
	var custom map[string]bool
	if err := json.Unmarshal(nested["custom"], &custom); err != nil || !custom["enabled"] {
		t.Errorf("pi custom lost: %s (%v)", doc["pi"], err)
	}
	if got := storesIn(t, path); len(got) != 1 || got[0].Name != "pi-work" {
		t.Errorf("stores = %+v", got)
	}
}

func jsonEqual(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return bytes.Equal(x, y)
}

func TestSetupPiStores_RejectsWithoutWriting(t *testing.T) {
	for _, tc := range []struct {
		name               string
		cfg                func(string) *config.Config
		old                string
		answer             string
		force, interactive bool
	}{
		{"invalid JSON", func(string) *config.Config { return &config.Config{} }, `{"pi":`, "y\n", true, false},
		{"invalid pi", func(string) *config.Config { return &config.Config{} }, `{"pi":[]}`, "y\n", true, false},
		{"invalid stores", func(string) *config.Config { return &config.Config{} }, `{"pi":{"stores":{}}}`, "y\n", true, false},
		{"invalid store entries", func(string) *config.Config { return &config.Config{} }, `{"pi":{"stores":["oops"]}}`, "y\n", true, false},
		{"sanitized collision", func(string) *config.Config {
			return &config.Config{Contexts: map[string]config.Context{"A B": {Profile: "one"}, "a-b": {Profile: "two"}}}
		}, `{}`, "y\n", true, false},
		{"default overlap", func(home string) *config.Config {
			return &config.Config{Contexts: map[string]config.Context{"bad": {Profile: "bad", ConfigDirs: map[string]string{"pi": filepath.Join(home, ".pi", "agent", "sessions", "child")}}}}
		}, `{}`, "y\n", true, false},
		{"store overlap", func(home string) *config.Config {
			return &config.Config{Contexts: map[string]config.Context{"one": {Profile: "one", ConfigDirs: map[string]string{"pi": filepath.Join(home, "nested")}}, "two": {Profile: "two", ConfigDirs: map[string]string{"pi": filepath.Join(home, "nested", "sessions")}}}}
		}, `{}`, "y\n", true, false},
		{"rejected prompt", func(string) *config.Config {
			return &config.Config{Contexts: map[string]config.Context{"work": {Profile: "work"}}}
		}, `{}`, "n\n", false, true},
		{"noninteractive", func(string) *config.Config {
			return &config.Config{Contexts: map[string]config.Context{"work": {Profile: "work"}}}
		}, `{}`, "y\n", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home, path := setupHome(t)
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(tc.old), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := setupRun(tc.cfg(home), home, tc.answer, tc.force, tc.interactive)
			if err == nil && tc.name != "rejected prompt" {
				t.Error("expected error")
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != tc.old {
				t.Errorf("changed file: %q", data)
			}
		})
	}
}

func TestSetupPiStores_RejectsCanonicalOverlap(t *testing.T) {
	home, path := setupHome(t)
	if err := os.MkdirAll(filepath.Join(home, ".pi", "agent", "sessions"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(home, ".pi", "agent", "sessions"), filepath.Join(home, "alias")); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Contexts: map[string]config.Context{"bad": {Profile: "bad", ConfigDirs: map[string]string{"pi": filepath.Join(home, "alias")}}}}
	if _, err := setupRun(cfg, home, "", true, false); err == nil || !strings.Contains(err.Error(), "overlap") {
		t.Errorf("error = %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("created file on overlap: %v", err)
	}
}

func TestSetupPiStores_RejectsSymlinkedPeerOverlap(t *testing.T) {
	home, path := setupHome(t)
	base := filepath.Join(home, "one")
	if err := os.MkdirAll(base, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(base, filepath.Join(home, "alias")); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Contexts: map[string]config.Context{
		"one": {Profile: "one", ConfigDirs: map[string]string{"pi": base}},
		"two": {Profile: "two", ConfigDirs: map[string]string{"pi": filepath.Join(home, "alias")}},
	}}
	if _, err := setupRun(cfg, home, "", true, false); err == nil || !strings.Contains(err.Error(), "overlap") {
		t.Errorf("error = %v, want overlap", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("created file on overlap: %v", err)
	}
}

func TestSetupPiStores_NonInteractiveDoesNotCreateFile(t *testing.T) {
	home, path := setupHome(t)
	cfg := &config.Config{Contexts: map[string]config.Context{"work": {Profile: "work"}}}
	if _, err := setupRun(cfg, home, "y\n", false, false); err == nil {
		t.Error("noninteractive setup should require --force")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("created file without confirmation: %v", err)
	}
}

func TestSetupPiStores_NoChangeDoesNotPromptOrWrite(t *testing.T) {
	home, path := setupHome(t)
	cfg := &config.Config{Contexts: map[string]config.Context{"work": {Profile: "work"}}}
	if _, err := setupRun(cfg, home, "", true, false); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	out, err := setupRun(cfg, home, "", false, false)
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) || strings.Contains(out, "[y/N]") {
		t.Errorf("unchanged output %q, file replaced", out)
	}
}
