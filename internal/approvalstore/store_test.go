package approvalstore

import (
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestStore_AddHasRead_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)

	key := "deadbeef"
	body := []byte("hello approval store")

	if s.Has(key) {
		t.Fatalf("Has(%q) = true before Add; want false", key)
	}
	if err := s.Add(key, body); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if !s.Has(key) {
		t.Fatalf("Has(%q) = false after Add; want true", key)
	}
	rec, err := s.Read(key)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if rec.Key != key {
		t.Errorf("Read.Key = %q, want %q", rec.Key, key)
	}
	if string(rec.Body) != string(body) {
		t.Errorf("Read.Body = %q, want %q", rec.Body, body)
	}
	if rec.ModTime.IsZero() {
		t.Errorf("Read.ModTime is zero")
	}
}

func TestStore_Remove_IdempotentAndMissing(t *testing.T) {
	s := NewStore(t.TempDir())
	if err := s.Remove("does-not-exist"); err != nil {
		t.Fatalf("Remove missing: %v", err)
	}
	_ = s.Add("k", []byte("v"))
	if err := s.Remove("k"); err != nil {
		t.Fatalf("Remove existing: %v", err)
	}
	if s.Has("k") {
		t.Errorf("Has after Remove = true; want false")
	}
	if err := s.Remove("k"); err != nil {
		t.Fatalf("Remove again: %v", err)
	}
}

func TestStore_List_EmptyAndSorted(t *testing.T) {
	s := NewStore(t.TempDir())
	recs, err := s.List()
	if err != nil {
		t.Fatalf("List on empty: %v", err)
	}
	if recs == nil {
		t.Errorf("List returned nil on empty store; want non-nil slice")
	}
	if len(recs) != 0 {
		t.Errorf("len(List) = %d on empty; want 0", len(recs))
	}

	for _, k := range []string{"c", "a", "b"} {
		if err := s.Add(k, []byte(k)); err != nil {
			t.Fatalf("Add %q: %v", k, err)
		}
	}
	recs, err = s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	got := []string{recs[0].Key, recs[1].Key, recs[2].Key}
	want := []string{"a", "b", "c"}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("List[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestStore_Add_Idempotent(t *testing.T) {
	s := NewStore(t.TempDir())
	if err := s.Add("k", []byte("first")); err != nil {
		t.Fatal(err)
	}
	if err := s.Add("k", []byte("second")); err != nil {
		t.Fatal(err)
	}
	rec, err := s.Read("k")
	if err != nil {
		t.Fatal(err)
	}
	if string(rec.Body) != "second" {
		t.Errorf("Body after re-Add = %q, want %q", rec.Body, "second")
	}
}

func TestStore_Add_EmptyKey(t *testing.T) {
	s := NewStore(t.TempDir())
	if err := s.Add("", []byte("x")); err == nil {
		t.Errorf("Add with empty key returned nil error; want error")
	}
}

func TestStore_Read_MissingKey(t *testing.T) {
	s := NewStore(t.TempDir())
	if _, err := s.Read("nope"); err == nil {
		t.Errorf("Read missing returned nil error")
	}
}

func TestStore_Permissions(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(filepath.Join(dir, "nested"))
	if err := s.Add("k", []byte("v")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, "nested"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != fs.FileMode(0o700) {
		t.Errorf("dir perm = %v, want 0700", info.Mode().Perm())
	}
	fi, err := os.Stat(filepath.Join(dir, "nested", "k"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != fs.FileMode(0o600) {
		t.Errorf("file perm = %v, want 0600", fi.Mode().Perm())
	}
}

func TestStore_Concurrent_DifferentKeys(t *testing.T) {
	s := NewStore(t.TempDir())
	var wg sync.WaitGroup
	const n = 50
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := []byte{byte('a' + i%26), byte('0' + i/26)}
			_ = s.Add(string(key), []byte("x"))
		}(i)
	}
	wg.Wait()
	recs, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) == 0 {
		t.Errorf("concurrent Add produced no records")
	}
}
