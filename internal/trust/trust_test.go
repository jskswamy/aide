package trust

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileHash(t *testing.T) {
	h := FileHash("/path/to/.aide.yaml", []byte("capabilities:\n  - go\n"))
	if len(h) != 64 { // hex-encoded SHA-256
		t.Errorf("expected 64-char hex hash, got %d chars", len(h))
	}
}

func TestPathHash(t *testing.T) {
	h := PathHash("/path/to/.aide.yaml")
	if len(h) != 64 {
		t.Errorf("expected 64-char hex hash, got %d chars", len(h))
	}
	fh := FileHash("/path/to/.aide.yaml", []byte("content"))
	if h == fh {
		t.Error("path hash should differ from file hash")
	}
}

func TestTrustAndCheck(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	path := "/project/.aide.yaml"
	content := []byte("capabilities:\n  - go\n")

	status := store.Check(path, content)
	if status != Untrusted {
		t.Errorf("expected Untrusted, got %v", status)
	}

	if err := store.Trust(path, content); err != nil {
		t.Fatal(err)
	}
	status = store.Check(path, content)
	if status != Trusted {
		t.Errorf("expected Trusted, got %v", status)
	}

	status = store.Check(path, []byte("capabilities:\n  - aws\n"))
	if status != Untrusted {
		t.Errorf("expected Untrusted after content change, got %v", status)
	}
}

func TestDenyAndCheck(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	path := "/project/.aide.yaml"
	content := []byte("capabilities:\n  - go\n")

	if err := store.Deny(path); err != nil {
		t.Fatal(err)
	}
	status := store.Check(path, content)
	if status != Denied {
		t.Errorf("expected Denied, got %v", status)
	}

	status = store.Check(path, []byte("different"))
	if status != Denied {
		t.Errorf("expected Denied with different content, got %v", status)
	}
}

func TestUntrust(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	path := "/project/.aide.yaml"
	content := []byte("caps")

	if err := store.Trust(path, content); err != nil {
		t.Fatal(err)
	}
	if err := store.Untrust(path, content); err != nil {
		t.Fatal(err)
	}
	status := store.Check(path, content)
	if status != Untrusted {
		t.Errorf("expected Untrusted after untrust, got %v", status)
	}
}

func TestTrustRemovesDeny(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	path := "/project/.aide.yaml"
	content := []byte("caps")

	if err := store.Deny(path); err != nil {
		t.Fatal(err)
	}
	if err := store.Trust(path, content); err != nil {
		t.Fatal(err)
	}
	status := store.Check(path, content)
	if status != Trusted {
		t.Errorf("expected Trusted after trust-over-deny, got %v", status)
	}
}

func TestDenyRemovesTrust(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	path := "/project/.aide.yaml"
	content := []byte("caps")

	if err := store.Trust(path, content); err != nil {
		t.Fatal(err)
	}
	if err := store.Deny(path); err != nil {
		t.Fatal(err)
	}
	status := store.Check(path, content)
	if status != Denied {
		t.Errorf("expected Denied after deny-over-trust, got %v", status)
	}
}

func TestStatusString(t *testing.T) {
	tests := []struct {
		status Status
		want   string
	}{
		{Trusted, "trusted"},
		{Denied, "denied"},
		{Untrusted, "untrusted"},
		{Status(99), "untrusted"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := tt.status.String()
			if got != tt.want {
				t.Errorf("Status(%d).String() = %q, want %q", tt.status, got, tt.want)
			}
		})
	}
}

func TestDefaultStore(t *testing.T) {
	// DefaultStore now delegates baseDir to approvalstore.DefaultRoot();
	// we observe its behavior via the public Trust/Check API, with the
	// XDG root redirected to a temp dir for hermeticity.
	t.Run("with XDG_DATA_HOME", func(t *testing.T) {
		root := t.TempDir()
		t.Setenv("XDG_DATA_HOME", root)
		s := DefaultStore()
		path := "/tmp/default-store-xdg"
		content := []byte("content")
		if err := s.Trust(path, content); err != nil {
			t.Fatal(err)
		}
		// The namespace contract: baseDir/aide/trust/<fileHash>
		expected := filepath.Join(root, "aide", "trust", FileHash(path, content))
		if _, err := os.Stat(expected); err != nil {
			t.Errorf("expected trust record at %s: %v", expected, err)
		}
	})

	t.Run("without XDG_DATA_HOME", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("XDG_DATA_HOME", "")
		t.Setenv("HOME", home)
		s := DefaultStore()
		path := "/tmp/default-store-home"
		content := []byte("content")
		if err := s.Trust(path, content); err != nil {
			t.Fatal(err)
		}
		expected := filepath.Join(home, ".local", "share", "aide", "trust", FileHash(path, content))
		if _, err := os.Stat(expected); err != nil {
			t.Errorf("expected trust record at %s: %v", expected, err)
		}
	})
}

func TestStore_Namespaces_TrustAndDeny(t *testing.T) {
	base := t.TempDir()
	s := NewStore(base)

	_ = s.Trust("/tmp/a", []byte("content-a"))
	_ = s.Deny("/tmp/b")

	trustDir := filepath.Join(base, "trust")
	denyDir := filepath.Join(base, "deny")
	if _, err := os.Stat(trustDir); err != nil {
		t.Errorf("trust/ namespace missing: %v", err)
	}
	if _, err := os.Stat(denyDir); err != nil {
		t.Errorf("deny/ namespace missing: %v", err)
	}

	trustEntries, _ := os.ReadDir(trustDir)
	denyEntries, _ := os.ReadDir(denyDir)
	if len(trustEntries) != 1 {
		t.Errorf("trust/ entry count = %d, want 1", len(trustEntries))
	}
	if len(denyEntries) != 1 {
		t.Errorf("deny/ entry count = %d, want 1", len(denyEntries))
	}
}
