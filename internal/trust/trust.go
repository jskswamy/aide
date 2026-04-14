// Package trust implements a content-addressed trust store for .aide.yaml files,
// following the same model as direnv's allow/deny mechanism. It is a sibling
// aggregate to the consent package under the User Approval bounded context;
// both delegate storage to internal/approvalstore.
package trust

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"

	"github.com/jskswamy/aide/internal/approvalstore"
)

// Status represents the trust state of a .aide.yaml file.
type Status int

// Trust statuses.
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

// Store manages trust/deny state for .aide.yaml files. Internally it owns
// two approvalstore.Store instances rooted at baseDir/trust and baseDir/deny.
type Store struct {
	trust *approvalstore.Store
	deny  *approvalstore.Store
}

// NewStore creates a trust store at the given base directory.
func NewStore(baseDir string) *Store {
	return &Store{
		trust: approvalstore.NewStore(filepath.Join(baseDir, "trust")),
		deny:  approvalstore.NewStore(filepath.Join(baseDir, "deny")),
	}
}

// DefaultStore returns a Store rooted at approvalstore.DefaultRoot().
func DefaultStore() *Store {
	return NewStore(approvalstore.DefaultRoot())
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
	if s.deny.Has(PathHash(path)) {
		return Denied
	}
	if s.trust.Has(FileHash(path, contents)) {
		return Trusted
	}
	return Untrusted
}

// Trust marks a file+content as trusted, removing any deny.
func (s *Store) Trust(path string, contents []byte) error {
	if err := s.trust.Add(FileHash(path, contents), []byte(path)); err != nil {
		return err
	}
	return s.deny.Remove(PathHash(path))
}

// Deny marks a path as denied, removing any trust at the same path. Because
// trust is keyed by content-hash but deny is keyed by path-hash, Deny cannot
// remove the trust record without knowing the content — that is delegated to
// the subsequent Check() which gives Denied precedence regardless of any
// stale trust record at the same path.
func (s *Store) Deny(path string) error {
	return s.deny.Add(PathHash(path), []byte(path))
}

// Untrust removes trust without creating a deny.
func (s *Store) Untrust(path string, contents []byte) error {
	return s.trust.Remove(FileHash(path, contents))
}
