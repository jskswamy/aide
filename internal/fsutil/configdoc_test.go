package fsutil

import (
	"path/filepath"
	"testing"
)

func TestTOMLRoundTrip(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.toml")
	doc, err := ReadTOML(missing)
	if err != nil || len(doc) != 0 {
		t.Fatalf("ReadTOML(missing) = %v, %v; want empty map", doc, err)
	}

	path := filepath.Join(dir, "config.toml")
	if err := WriteTOML(path, map[string]any{"model": "gpt-5"}); err != nil {
		t.Fatalf("WriteTOML() error = %v", err)
	}
	got, err := ReadTOML(path)
	if err != nil || got["model"] != "gpt-5" {
		t.Fatalf("ReadTOML() = %v, %v", got, err)
	}
}

func TestJSONCRoundTrip(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.jsonc")
	if m, err := ReadJSONCMap(missing); err != nil || len(m) != 0 {
		t.Fatalf("ReadJSONCMap(missing) = %v, %v", m, err)
	}
	if rm, err := ReadJSONCRawMap(missing); err != nil || len(rm) != 0 {
		t.Fatalf("ReadJSONCRawMap(missing) = %v, %v", rm, err)
	}

	path := filepath.Join(dir, "config.jsonc")
	if err := WriteJSON(path, map[string]string{"k": "v"}); err != nil {
		t.Fatalf("WriteJSON() error = %v", err)
	}
	got, err := ReadJSONCMap(path)
	if err != nil || got["k"] != "v" {
		t.Fatalf("ReadJSONCMap() = %v, %v", got, err)
	}
	raw, err := ReadJSONCRawMap(path)
	if err != nil || string(raw["k"]) != `"v"` {
		t.Fatalf("ReadJSONCRawMap() = %v, %v", raw, err)
	}
}
