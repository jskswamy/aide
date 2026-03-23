// pkg/seatbelt/path_helpers_test.go
package seatbelt

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExistsOrUnderHome_ExistingPath(t *testing.T) {
	home := t.TempDir()
	existing := filepath.Join(home, ".config")
	if err := os.Mkdir(existing, 0o755); err != nil {
		t.Fatal(err)
	}
	if !ExistsOrUnderHome(home, existing) {
		t.Error("expected true for existing path")
	}
}

func TestExistsOrUnderHome_NonExistentUnderHome(t *testing.T) {
	home := t.TempDir()
	nonExistent := filepath.Join(home, ".agent-config")
	if !ExistsOrUnderHome(home, nonExistent) {
		t.Error("expected true for non-existent path under home")
	}
}

func TestExistsOrUnderHome_NonExistentOutsideHome(t *testing.T) {
	home := t.TempDir()
	if ExistsOrUnderHome(home, "/opt/agent-config") {
		t.Error("expected false for non-existent path outside home")
	}
}

func TestExistsOrUnderHome_ExistingOutsideHome(t *testing.T) {
	home := t.TempDir()
	outside := t.TempDir() // exists but not under home
	if !ExistsOrUnderHome(home, outside) {
		t.Error("expected true for existing path even outside home")
	}
}

func TestExistsOrUnderHome_HomePrefixBoundary(t *testing.T) {
	// /tmp/home vs /tmp/homefoo — must not match
	home := t.TempDir()
	sibling := home + "foo"
	if ExistsOrUnderHome(home, sibling) {
		t.Errorf("should not match sibling dir %q when home is %q", sibling, home)
	}
}
