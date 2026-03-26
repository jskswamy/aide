package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jskswamy/aide/internal/trust"
	"gopkg.in/yaml.v3"
)

func TestWriteProjectOverride_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ProjectConfigFileName)

	po := &ProjectOverride{
		Capabilities: []string{"k8s"},
		Env:          map[string]string{"FOO": "bar"},
	}
	if err := WriteProjectOverride(path, po); err != nil {
		t.Fatalf("WriteProjectOverride() error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	var loaded ProjectOverride
	if err := yaml.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if len(loaded.Capabilities) != 1 || loaded.Capabilities[0] != "k8s" {
		t.Errorf("expected capabilities [k8s], got %v", loaded.Capabilities)
	}
	if loaded.Env["FOO"] != "bar" {
		t.Errorf("expected env FOO=bar, got %v", loaded.Env)
	}
}

func TestWriteProjectOverride_Atomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ProjectConfigFileName)

	po1 := &ProjectOverride{Capabilities: []string{"docker"}}
	if err := WriteProjectOverride(path, po1); err != nil {
		t.Fatalf("initial write error = %v", err)
	}

	po2 := &ProjectOverride{Capabilities: []string{"k8s", "aws"}}
	if err := WriteProjectOverride(path, po2); err != nil {
		t.Fatalf("update write error = %v", err)
	}

	loaded, err := loadProjectOverride(path)
	if err != nil {
		t.Fatalf("loadProjectOverride() error = %v", err)
	}
	if len(loaded.Capabilities) != 2 {
		t.Errorf("expected 2 capabilities after update, got %v", loaded.Capabilities)
	}
}

func TestFindProjectConfigForWrite_ExistingFile(t *testing.T) {
	dir := t.TempDir()
	aidePath := filepath.Join(dir, ProjectConfigFileName)
	if err := os.WriteFile(aidePath, []byte("agent: claude\n"), 0644); err != nil {
		t.Fatal(err)
	}
	subdir := filepath.Join(dir, "sub")
	if err := os.MkdirAll(subdir, 0755); err != nil {
		t.Fatal(err)
	}

	got := FindProjectConfigForWrite(subdir)
	if got != aidePath {
		t.Errorf("expected %q, got %q", aidePath, got)
	}
}

func TestFindProjectConfigForWrite_GitRoot(t *testing.T) {
	dir := t.TempDir()
	gitDir := filepath.Join(dir, ".git")
	if err := os.MkdirAll(gitDir, 0755); err != nil {
		t.Fatal(err)
	}
	subdir := filepath.Join(dir, "src", "pkg")
	if err := os.MkdirAll(subdir, 0755); err != nil {
		t.Fatal(err)
	}

	got := FindProjectConfigForWrite(subdir)
	expected := filepath.Join(dir, ProjectConfigFileName)
	if got != expected {
		t.Errorf("expected %q (git root), got %q", expected, got)
	}
}

func TestFindProjectConfigForWrite_FallbackToCwd(t *testing.T) {
	dir := t.TempDir()
	got := FindProjectConfigForWrite(dir)
	expected := filepath.Join(dir, ProjectConfigFileName)
	if got != expected {
		t.Errorf("expected %q (cwd fallback), got %q", expected, got)
	}
}

func TestWriteProjectOverrideWithTrust_AutoReTrust(t *testing.T) {
	dir := t.TempDir()
	trustDir := t.TempDir()
	store := trust.NewStore(trustDir)

	path := filepath.Join(dir, ProjectConfigFileName)

	// Write initial content and trust it.
	po1 := &ProjectOverride{Capabilities: []string{"go"}}
	if err := WriteProjectOverride(path, po1); err != nil {
		t.Fatalf("initial write error: %v", err)
	}
	content1, _ := os.ReadFile(path)
	if err := store.Trust(path, content1); err != nil {
		t.Fatalf("trust error: %v", err)
	}
	if store.Check(path, content1) != trust.Trusted {
		t.Fatal("should be trusted before modification")
	}

	// Write new content via trust-aware writer.
	po2 := &ProjectOverride{Capabilities: []string{"go", "docker"}}
	if err := WriteProjectOverrideWithTrust(path, po2, store); err != nil {
		t.Fatalf("trust-aware write error: %v", err)
	}

	// New content should be auto-re-trusted.
	content2, _ := os.ReadFile(path)
	if store.Check(path, content2) != trust.Trusted {
		t.Error("should be auto-re-trusted after aide-initiated modification")
	}
}

func TestWriteProjectOverrideWithTrust_NoAutoReTrustIfExternallyModified(t *testing.T) {
	dir := t.TempDir()
	trustDir := t.TempDir()
	store := trust.NewStore(trustDir)

	path := filepath.Join(dir, ProjectConfigFileName)

	// Write initial content and trust it.
	po1 := &ProjectOverride{Capabilities: []string{"go"}}
	if err := WriteProjectOverride(path, po1); err != nil {
		t.Fatalf("initial write error: %v", err)
	}
	content1, _ := os.ReadFile(path)
	if err := store.Trust(path, content1); err != nil {
		t.Fatalf("trust error: %v", err)
	}

	// Simulate external modification (e.g., git pull changed the file).
	externalContent := []byte("capabilities:\n  - go\n  - aws\n")
	if err := os.WriteFile(path, externalContent, 0644); err != nil {
		t.Fatalf("external write error: %v", err)
	}
	// External content should be untrusted.
	if store.Check(path, externalContent) != trust.Untrusted {
		t.Fatal("externally modified content should be untrusted")
	}

	// Now aide writes via trust-aware writer.
	po2 := &ProjectOverride{Capabilities: []string{"go", "docker"}}
	if err := WriteProjectOverrideWithTrust(path, po2, store); err != nil {
		t.Fatalf("trust-aware write error: %v", err)
	}

	// New content should NOT be auto-re-trusted (pre-modification was untrusted).
	content2, _ := os.ReadFile(path)
	if store.Check(path, content2) != trust.Untrusted {
		t.Error("should NOT be auto-re-trusted when pre-modification content was untrusted")
	}
}

func TestWriteProjectOverrideWithTrust_NewFile(t *testing.T) {
	dir := t.TempDir()
	trustDir := t.TempDir()
	store := trust.NewStore(trustDir)

	path := filepath.Join(dir, ProjectConfigFileName)

	// Write to a path that doesn't exist yet — no prior trust.
	po := &ProjectOverride{Capabilities: []string{"go"}}
	if err := WriteProjectOverrideWithTrust(path, po, store); err != nil {
		t.Fatalf("trust-aware write error: %v", err)
	}

	// New file should NOT be auto-trusted (no prior trust).
	content, _ := os.ReadFile(path)
	if store.Check(path, content) != trust.Untrusted {
		t.Error("new file should not be auto-trusted")
	}
}
