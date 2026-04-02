package trust

import (
	"os"
	"path/filepath"
	"strings"
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
	t.Run("with XDG_DATA_HOME", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", "/custom/data")
		s := DefaultStore()
		if s.baseDir != "/custom/data/aide" {
			t.Errorf("DefaultStore().baseDir = %q, want /custom/data/aide", s.baseDir)
		}
	})

	t.Run("without XDG_DATA_HOME", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", "")
		s := DefaultStore()
		if !strings.HasSuffix(s.baseDir, ".local/share/aide") {
			t.Errorf("DefaultStore().baseDir = %q, want suffix .local/share/aide", s.baseDir)
		}
	})
}

func TestFileExists(t *testing.T) {
	t.Run("exists", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "test")
		if err := os.WriteFile(f, []byte("data"), 0o600); err != nil {
			t.Fatal(err)
		}
		if !fileExists(f) {
			t.Error("fileExists() = false for existing file")
		}
	})

	t.Run("missing", func(t *testing.T) {
		if fileExists("/nonexistent/path") {
			t.Error("fileExists() = true for missing path")
		}
	})

	t.Run("directory", func(t *testing.T) {
		d := t.TempDir()
		if !fileExists(d) {
			t.Error("fileExists() = false for directory")
		}
	})
}

func TestAtomicWrite(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "testfile")
		err := atomicWrite(path, []byte("hello"))
		if err != nil {
			t.Fatalf("atomicWrite() error = %v", err)
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "hello" {
			t.Errorf("file content = %q, want %q", got, "hello")
		}
	})

	t.Run("read-only directory", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "readonly")
		if err := os.MkdirAll(dir, 0o500); err != nil {
			t.Fatal(err)
		}
		err := atomicWrite(filepath.Join(dir, "fail"), []byte("data"))
		if err == nil {
			t.Error("atomicWrite() expected error for read-only directory")
		}
	})
}
