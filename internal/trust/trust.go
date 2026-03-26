package trust

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
)

type Status int

const (
	Untrusted Status = iota
	Trusted
	Denied
)

func (s Status) String() string {
	switch s {
	case Trusted:
		return "trusted"
	case Denied:
		return "denied"
	default:
		return "untrusted"
	}
}

// Store manages trust/deny state for .aide.yaml files.
type Store struct {
	baseDir string
}

// NewStore creates a trust store at the given base directory.
func NewStore(baseDir string) *Store {
	return &Store{baseDir: baseDir}
}

// DefaultStore returns a Store using XDG_DATA_HOME/aide.
func DefaultStore() *Store {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".local", "share")
	}
	return NewStore(filepath.Join(base, "aide"))
}

// FileHash computes SHA-256(path + "\n" + contents).
func FileHash(path string, contents []byte) string {
	h := sha256.New()
	h.Write([]byte(path))
	h.Write([]byte("\n"))
	h.Write(contents)
	return hex.EncodeToString(h.Sum(nil))
}

// PathHash computes SHA-256(path + "\n").
func PathHash(path string) string {
	h := sha256.New()
	h.Write([]byte(path))
	h.Write([]byte("\n"))
	return hex.EncodeToString(h.Sum(nil))
}

// Check returns the trust status for a file with given content.
func (s *Store) Check(path string, contents []byte) Status {
	ph := PathHash(path)
	if fileExists(filepath.Join(s.baseDir, "deny", ph)) {
		return Denied
	}
	fh := FileHash(path, contents)
	if fileExists(filepath.Join(s.baseDir, "trust", fh)) {
		return Trusted
	}
	return Untrusted
}

// Trust marks a file+content as trusted, removing any deny.
func (s *Store) Trust(path string, contents []byte) error {
	fh := FileHash(path, contents)
	trustDir := filepath.Join(s.baseDir, "trust")
	if err := os.MkdirAll(trustDir, 0o700); err != nil {
		return err
	}
	if err := atomicWrite(filepath.Join(trustDir, fh), []byte(path)); err != nil {
		return err
	}
	ph := PathHash(path)
	os.Remove(filepath.Join(s.baseDir, "deny", ph))
	return nil
}

// Deny marks a path as denied, removing any trust.
func (s *Store) Deny(path string) error {
	ph := PathHash(path)
	denyDir := filepath.Join(s.baseDir, "deny")
	if err := os.MkdirAll(denyDir, 0o700); err != nil {
		return err
	}
	return atomicWrite(filepath.Join(denyDir, ph), []byte(path))
}

// Untrust removes trust without creating a deny.
func (s *Store) Untrust(path string, contents []byte) error {
	fh := FileHash(path, contents)
	return os.Remove(filepath.Join(s.baseDir, "trust", fh))
}

// atomicWrite writes data to a temp file then renames.
func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".aide-trust-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	f.Close()
	return os.Rename(tmp, path)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
