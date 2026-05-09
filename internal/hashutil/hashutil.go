// Package hashutil produces content-addressed digests with a single
// length-prefix encoding. Length prefixing makes the encoding injective:
// values that differ only in where their internal separators fall cannot
// collide. All callers in the User-Approval bounded context (trust,
// consent, evidence digests) build their keys through Builder so the
// version tag and field framing are owned in one place.
package hashutil

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
)

// Builder accumulates length-prefixed fields into a SHA-256.
type Builder struct {
	h hash.Hash
}

// New starts a Builder and writes the version tag as the first framed
// segment. Distinct version tags guarantee distinct namespaces: a
// "trust-v1" digest cannot match a "consent-v1" digest of the same
// fields.
func New(version string) *Builder {
	b := &Builder{h: sha256.New()}
	b.h.Write([]byte(version))
	b.h.Write([]byte("\n"))
	return b
}

// Field writes a length-prefixed UTF-8 field and returns b for chaining.
func (b *Builder) Field(s string) *Builder {
	fmt.Fprintf(b.h, "%d:", len(s))
	b.h.Write([]byte(s))
	return b
}

// Bytes writes a length-prefixed byte slice. Bytes and Field share an
// encoding so callers can mix them freely.
func (b *Builder) Bytes(p []byte) *Builder {
	fmt.Fprintf(b.h, "%d:", len(p))
	b.h.Write(p)
	return b
}

// HexSum returns the lowercase hex SHA-256 of the accumulated input.
func (b *Builder) HexSum() string {
	return hex.EncodeToString(b.h.Sum(nil))
}
