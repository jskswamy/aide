package capability

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestMarker_FileMatch(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "uv.lock", "")
	m := Marker{File: "uv.lock"}
	if !m.Match(dir) {
		t.Errorf("Marker{File: uv.lock}.Match = false; want true")
	}
	m2 := Marker{File: "missing.lock"}
	if m2.Match(dir) {
		t.Errorf("Marker on missing file matched")
	}
}

func TestMarker_ContainsMatch(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pyproject.toml", "[tool.poetry]\nname=\"x\"\n")
	m := Marker{Contains: ContainsSpec{File: "pyproject.toml", Pattern: "[tool.poetry]"}}
	if !m.Match(dir) {
		t.Errorf("Contains marker did not match present pattern")
	}
	m2 := Marker{Contains: ContainsSpec{File: "pyproject.toml", Pattern: "[tool.uv]"}}
	if m2.Match(dir) {
		t.Errorf("Contains marker matched absent pattern")
	}
}

func TestMarker_GlobMatch(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.tf", "")
	m := Marker{GlobPath: "*.tf"}
	if !m.Match(dir) {
		t.Errorf("Glob marker did not match *.tf")
	}
}

func TestMarker_Validate_ExactlyOneFieldSet(t *testing.T) {
	cases := []struct {
		name  string
		m     Marker
		valid bool
	}{
		{"file only", Marker{File: "x"}, true},
		{"contains only", Marker{Contains: ContainsSpec{File: "x", Pattern: "y"}}, true},
		{"glob only", Marker{GlobPath: "*.x"}, true},
		{"empty", Marker{}, false},
		{"file+glob", Marker{File: "x", GlobPath: "*.x"}, false},
	}
	for _, tc := range cases {
		err := tc.m.Validate()
		gotValid := err == nil
		if gotValid != tc.valid {
			t.Errorf("%s: Validate err=%v, wantValid=%v", tc.name, err, tc.valid)
		}
	}
}

func TestMarker_File_RejectsDirectory(t *testing.T) {
	dir := t.TempDir()
	// Create a directory NAMED like a lockfile; Marker.File must not match it.
	if err := os.Mkdir(filepath.Join(dir, "uv.lock"), 0o700); err != nil {
		t.Fatal(err)
	}
	m := Marker{File: "uv.lock"}
	if m.Match(dir) {
		t.Errorf("Marker{File: uv.lock}.Match matched a directory; want false (files only)")
	}
}

func TestMarker_ContainsMatch_ReadBoundary(t *testing.T) {
	// Pattern present inside the bounded-read window matches; pattern past
	// the window does not. The boundary lives at markerMaxReadSize (64 KiB).
	dir := t.TempDir()

	// Pattern fully within the first 64 KiB -> matches.
	early := make([]byte, 1024)
	copy(early, []byte("[tool.uv]"))
	writeFile(t, dir, "inside.toml", string(early))
	inside := Marker{Contains: ContainsSpec{File: "inside.toml", Pattern: "[tool.uv]"}}
	if !inside.Match(dir) {
		t.Errorf("pattern inside the read window did not match")
	}

	// Pattern past the 64 KiB boundary -> does not match (documented behavior).
	pad := make([]byte, markerMaxReadSize)
	body := append(pad, []byte("[tool.uv]")...)
	writeFile(t, dir, "outside.toml", string(body))
	outside := Marker{Contains: ContainsSpec{File: "outside.toml", Pattern: "[tool.uv]"}}
	if outside.Match(dir) {
		t.Errorf("pattern past %d-byte read window matched; bounded-read design says it should not",
			markerMaxReadSize)
	}
}

func TestMarker_DirExists_Match(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "k8s"), 0o700); err != nil {
		t.Fatal(err)
	}
	m := Marker{DirExists: "k8s"}
	if !m.Match(dir) {
		t.Errorf("DirExists marker did not match existing directory")
	}
}

func TestMarker_DirExists_RejectsFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "k8s", "")
	m := Marker{DirExists: "k8s"}
	if m.Match(dir) {
		t.Errorf("DirExists matched a file at the same name; want directory only")
	}
}

func TestMarker_DirExists_Missing(t *testing.T) {
	m := Marker{DirExists: "k8s"}
	if m.Match(t.TempDir()) {
		t.Errorf("DirExists matched a missing directory")
	}
}

func TestMarker_GlobContains_Match(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "deploy.yaml", "apiVersion: apps/v1\nkind: Deployment\n")
	m := Marker{GlobContains: GlobContainsSpec{Glob: "*.yaml", Pattern: "apiVersion:"}}
	if !m.Match(dir) {
		t.Errorf("GlobContains did not match yaml containing pattern")
	}
}

func TestMarker_GlobContains_NoMatch(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "deploy.yaml", "name: foo\n")
	m := Marker{GlobContains: GlobContainsSpec{Glob: "*.yaml", Pattern: "apiVersion:"}}
	if m.Match(dir) {
		t.Errorf("GlobContains matched yaml without pattern")
	}
}

func TestMarker_GlobContains_FilesCap(t *testing.T) {
	dir := t.TempDir()
	// 60 yaml files, none contain apiVersion. Scan must not exceed the cap
	// and must not return a false positive.
	for i := 0; i < 60; i++ {
		writeFile(t, dir, fmt.Sprintf("f%02d.yaml", i), "name: foo\n")
	}
	m := Marker{GlobContains: GlobContainsSpec{Glob: "*.yaml", Pattern: "apiVersion:"}}
	if m.Match(dir) {
		t.Errorf("GlobContains matched despite no file containing pattern")
	}
}

func TestAnyMarkerMatches_Empty(t *testing.T) {
	if AnyMarkerMatches(t.TempDir(), nil) {
		t.Errorf("AnyMarkerMatches on empty list returned true")
	}
}

func TestAnyMarkerMatches_FirstMatchWins(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Dockerfile", "")
	ms := []Marker{{File: "Dockerfile"}, {File: "never.txt"}}
	if !AnyMarkerMatches(dir, ms) {
		t.Errorf("AnyMarkerMatches did not return true on first-marker match")
	}
}

func TestAnyMarkerMatches_NoneMatch(t *testing.T) {
	dir := t.TempDir()
	ms := []Marker{{File: "a"}, {File: "b"}}
	if AnyMarkerMatches(dir, ms) {
		t.Errorf("AnyMarkerMatches returned true when none matched")
	}
}

func TestAllMarkersMatch_Empty(t *testing.T) {
	if AllMarkersMatch(t.TempDir(), nil) {
		t.Errorf("AllMarkersMatch on empty list returned true; want false")
	}
}

func TestAllMarkersMatch_AllMatch(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a", "")
	writeFile(t, dir, "b", "")
	ms := []Marker{{File: "a"}, {File: "b"}}
	if !AllMarkersMatch(dir, ms) {
		t.Errorf("AllMarkersMatch did not return true when all matched")
	}
}

func TestAllMarkersMatch_OneFails(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a", "")
	ms := []Marker{{File: "a"}, {File: "b"}}
	if AllMarkersMatch(dir, ms) {
		t.Errorf("AllMarkersMatch returned true with one marker failing")
	}
}
