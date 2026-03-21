package seatbelt

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSeatbeltPath_Directory(t *testing.T) {
	dir := t.TempDir()
	got := SeatbeltPath(dir)
	want := `(subpath "` + dir + `")`
	if got != want {
		t.Errorf("SeatbeltPath(%q) = %q, want %q", dir, got, want)
	}
}

func TestSeatbeltPath_File(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(f, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	got := SeatbeltPath(f)
	want := `(literal "` + f + `")`
	if got != want {
		t.Errorf("SeatbeltPath(%q) = %q, want %q", f, got, want)
	}
}

func TestExpandGlobs_NoGlob(t *testing.T) {
	got := ExpandGlobs([]string{"/tmp/foo"})
	if len(got) != 1 || got[0] != "/tmp/foo" {
		t.Errorf("expected [/tmp/foo], got %v", got)
	}
}
