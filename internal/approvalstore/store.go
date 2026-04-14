// Package approvalstore provides a content-addressed file-backed set used
// by the trust and consent aggregates under the User Approval bounded
// context. It has no domain concepts — callers supply the hex-encoded
// key and an opaque body; the store persists the pair under its base
// directory.
package approvalstore

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Store is a content-addressed set backed by a directory of files.
type Store struct {
	baseDir string
}

// Record is the result of reading a key from the store.
type Record struct {
	Key     string
	Body    []byte
	ModTime time.Time
}

// NewStore creates a Store rooted at baseDir. The directory is created
// on first write.
func NewStore(baseDir string) *Store {
	return &Store{baseDir: baseDir}
}

// DefaultRoot returns XDG_DATA_HOME/aide (or ~/.local/share/aide when
// XDG_DATA_HOME is unset). Aggregates should nest their namespaces
// underneath this root.
func DefaultRoot() string {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(base, "aide")
}

// Has reports whether a record with the given key exists.
func (s *Store) Has(key string) bool {
	_, err := os.Stat(filepath.Join(s.baseDir, key))
	return err == nil
}

// Add writes body to the record at key, creating the base directory if
// needed. Add is idempotent: re-adding the same key overwrites the body.
func (s *Store) Add(key string, body []byte) error {
	if key == "" {
		return errors.New("approvalstore: empty key")
	}
	if err := os.MkdirAll(s.baseDir, 0o700); err != nil {
		return err
	}
	return atomicWrite(filepath.Join(s.baseDir, key), body)
}

// Remove deletes the record for key. Missing keys are a no-op.
func (s *Store) Remove(key string) error {
	err := os.Remove(filepath.Join(s.baseDir, key))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// Read returns the record for key, or os.ErrNotExist if absent.
func (s *Store) Read(key string) (Record, error) {
	path := filepath.Join(s.baseDir, key)
	body, err := os.ReadFile(path)
	if err != nil {
		return Record{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return Record{}, err
	}
	return Record{Key: key, Body: body, ModTime: info.ModTime()}, nil
}

// List returns all records in the store sorted by key. An empty store
// returns an empty (non-nil) slice.
func (s *Store) List() ([]Record, error) {
	entries, err := os.ReadDir(s.baseDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []Record{}, nil
		}
		return nil, err
	}
	keys := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		keys = append(keys, e.Name())
	}
	sort.Strings(keys)
	out := make([]Record, 0, len(keys))
	for _, k := range keys {
		rec, err := s.Read(k)
		if err != nil {
			continue // skip unreadable records
		}
		out = append(out, rec)
	}
	return out, nil
}

// atomicWrite writes data to a temp file in the same directory then
// renames it over path. File permissions are 0o600.
func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".aide-approval-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}
