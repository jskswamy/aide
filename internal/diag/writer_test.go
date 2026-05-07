package diag

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWrite_HappyPath(t *testing.T) {
	dir := t.TempDir()
	w := &Writer{CacheDir: dir}
	stderr := &bytes.Buffer{}
	path, err := w.Write("hello world", stderr)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(path, dir) {
		t.Errorf("path %q not under %q", path, dir)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "hello world" {
		t.Errorf("file content mismatch: %q", got)
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr should be empty on happy path, got: %q", stderr.String())
	}
}

func TestWrite_FallsBackToStderrOnMkdirFailure(t *testing.T) {
	w := &Writer{CacheDir: "/nonexistent/cannot-create"}
	w.MkdirAll = func(string, os.FileMode) error { return errors.New("simulated permission denied") }
	stderr := &bytes.Buffer{}
	path, err := w.Write("body content", stderr)
	if err == nil {
		t.Error("expected non-nil error")
	}
	if path != "" {
		t.Errorf("expected empty path on fallback, got %q", path)
	}
	if !strings.Contains(stderr.String(), "body content") {
		t.Errorf("stderr should contain report body: %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "warning") {
		t.Errorf("stderr should explain it's a fallback: %q", stderr.String())
	}
}

func TestPath_FormatHasTimestampAndShortHash(t *testing.T) {
	w := &Writer{CacheDir: "/tmp/x"}
	p := w.path("cwd|argv|123")
	base := filepath.Base(p)
	if !strings.HasSuffix(base, ".md") {
		t.Errorf("not .md: %s", base)
	}
	// Format: 2026-05-07T12-04-05Z-ab12cd34.md → ~30 chars total
	if len(base) < 25 {
		t.Errorf("name too short: %s", base)
	}
	if !strings.Contains(base, "Z-") {
		t.Errorf("missing 'Z-' separator between timestamp and hash: %s", base)
	}
}

func TestPath_DifferentSeedsProduceDifferentNames(t *testing.T) {
	w := &Writer{CacheDir: "/tmp/x"}
	p1 := w.path("seed-a")
	p2 := w.path("seed-b")
	if p1 == p2 {
		t.Errorf("expected different filenames for different seeds: %s == %s", p1, p2)
	}
}
