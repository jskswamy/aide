package seatbelt

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPath_Directory(t *testing.T) {
	dir := t.TempDir()
	got := Path(dir)
	want := `(subpath "` + dir + `")`
	if got != want {
		t.Errorf("Path(%q) = %q, want %q", dir, got, want)
	}
}

func TestPath_File(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(f, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	got := Path(f)
	want := `(literal "` + f + `")`
	if got != want {
		t.Errorf("Path(%q) = %q, want %q", f, got, want)
	}
}

func TestHomeSubpath(t *testing.T) {
	got := HomeSubpath("/home/user", ".config/gh")
	want := `(subpath "/home/user/.config/gh")`
	if got != want {
		t.Errorf("HomeSubpath = %q, want %q", got, want)
	}
}

func TestHomeLiteral(t *testing.T) {
	got := HomeLiteral("/home/user", ".bashrc")
	want := `(literal "/home/user/.bashrc")`
	if got != want {
		t.Errorf("HomeLiteral = %q, want %q", got, want)
	}
}

func TestHomePrefix(t *testing.T) {
	got := HomePrefix("/home/user", ".config/")
	want := `(prefix "/home/user/.config")`
	if got != want {
		t.Errorf("HomePrefix = %q, want %q", got, want)
	}
}

func TestExpandGlobs_NoGlob(t *testing.T) {
	got := ExpandGlobs([]string{"/tmp/foo"})
	if len(got) != 1 || got[0] != "/tmp/foo" {
		t.Errorf("expected [/tmp/foo], got %v", got)
	}
}

func TestExpandGlobs_WithGlob(t *testing.T) {
	dir := t.TempDir()
	// Create some files matching a glob pattern
	for _, name := range []string{"a.txt", "b.txt", "c.log"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	patterns := []string{
		filepath.Join(dir, "*.txt"),
		"/some/literal/path",
	}
	got := ExpandGlobs(patterns)

	// Should have 2 expanded txt files + 1 literal path
	if len(got) != 3 {
		t.Fatalf("expected 3 results, got %d: %v", len(got), got)
	}
	if got[len(got)-1] != "/some/literal/path" {
		t.Errorf("last entry should be literal path, got %q", got[len(got)-1])
	}
}

func TestExpandGlobs_NoMatch(t *testing.T) {
	dir := t.TempDir()
	// Glob that matches nothing
	got := ExpandGlobs([]string{filepath.Join(dir, "*.xyz")})
	if len(got) != 0 {
		t.Errorf("expected empty result for non-matching glob, got %v", got)
	}
}

func TestExpandGlobs_Nil(t *testing.T) {
	got := ExpandGlobs(nil)
	if len(got) != 0 {
		t.Errorf("expected empty result for nil input, got %v", got)
	}
}
