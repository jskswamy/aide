package trust

import "testing"

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
