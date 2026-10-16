package statedir

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPathDefaultsToLocalState(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	home := t.TempDir()
	got := Path(home, "managed.json")
	want := filepath.Join(home, ".local", "state", "aide", "managed.json")
	if got != want {
		t.Fatalf("Path = %q, want %q", got, want)
	}
}

func TestPathHonorsXDGStateHome(t *testing.T) {
	home := t.TempDir()
	xdg := filepath.Join(t.TempDir(), "state")
	t.Setenv("XDG_STATE_HOME", xdg)
	got := Path(home, "preferences.json")
	want := filepath.Join(xdg, "aide", "preferences.json")
	if got != want {
		t.Fatalf("Path = %q, want %q", got, want)
	}
}

func TestPathIgnoresRelativeXDGStateHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_STATE_HOME", "relative/state")
	got := Path(home, "managed.json")
	want := filepath.Join(home, ".local", "state", "aide", "managed.json")
	if got != want {
		t.Fatalf("Path = %q, want %q (relative XDG_STATE_HOME must be ignored)", got, want)
	}
}

// A state file already at the old default stays in use when
// XDG_STATE_HOME points elsewhere and holds no file yet, so setting
// the variable never orphans ownership or preferences.
func TestPathFallsBackToExistingLegacyFile(t *testing.T) {
	home := t.TempDir()
	legacy := filepath.Join(home, ".local", "state", "aide", "managed.json")
	if err := os.MkdirAll(filepath.Dir(legacy), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	xdg := filepath.Join(t.TempDir(), "state")
	t.Setenv("XDG_STATE_HOME", xdg)
	if got := Path(home, "managed.json"); got != legacy {
		t.Fatalf("Path = %q, want legacy %q", got, legacy)
	}

	// Once the XDG location holds the file, it wins.
	moved := filepath.Join(xdg, "aide", "managed.json")
	if err := os.MkdirAll(filepath.Dir(moved), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(moved, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := Path(home, "managed.json"); got != moved {
		t.Fatalf("Path = %q, want %q", got, moved)
	}
}
