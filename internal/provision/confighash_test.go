package provision_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/provision"
)

func TestConfigHashStableForSameBytes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("hello: world\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a, err := provision.ConfigHash(path)
	if err != nil {
		t.Fatal(err)
	}
	b, err := provision.ConfigHash(path)
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Errorf("hashes differ: %s vs %s", a, b)
	}
	if !strings.HasPrefix(a, "sha256:") {
		t.Errorf("expected sha256: prefix, got %q", a)
	}
}

func TestConfigHashChangesWithBytes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	_ = os.WriteFile(path, []byte("a: 1\n"), 0o600)
	h1, _ := provision.ConfigHash(path)
	_ = os.WriteFile(path, []byte("a: 2\n"), 0o600)
	h2, _ := provision.ConfigHash(path)
	if h1 == h2 {
		t.Error("expected different hashes")
	}
}

func TestConfigHashMissingFileReturnsEmpty(t *testing.T) {
	h, err := provision.ConfigHash(filepath.Join(t.TempDir(), "nope.yaml"))
	if err != nil {
		t.Fatalf("missing config should not error, got %v", err)
	}
	if h != "" {
		t.Errorf("expected empty hash for missing file, got %q", h)
	}
}
