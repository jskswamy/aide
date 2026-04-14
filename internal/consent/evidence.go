// Package consent stores user approvals of auto-detected toolchain
// variant selections. It is a sibling aggregate to the trust package
// within the User Approval bounded context. Storage mechanics are
// reused via internal/approvalstore.
package consent

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"sort"
	"strings"
)

// MarkerMatch records a single detection marker and whether it matched
// in the scanned project root.
type MarkerMatch struct {
	Kind    string // "file" | "contains" | "glob"
	Target  string // e.g. "uv.lock" or "pyproject.toml:[tool.uv]"
	Matched bool
}

// Evidence is the full detection result for one capability: which
// variants were selected and which markers drove the selection.
type Evidence struct {
	Variants []string
	Matches  []MarkerMatch
}

// Digest returns a SHA-256 over a canonicalized representation of the
// evidence. Order of Variants and Matches does not affect the digest;
// match flips and variant set changes do.
func (e Evidence) Digest() string {
	variants := append([]string(nil), e.Variants...)
	sort.Strings(variants)

	matches := append([]MarkerMatch(nil), e.Matches...)
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Kind != matches[j].Kind {
			return matches[i].Kind < matches[j].Kind
		}
		if matches[i].Target != matches[j].Target {
			return matches[i].Target < matches[j].Target
		}
		return !matches[i].Matched && matches[j].Matched
	})

	h := sha256.New()
	h.Write([]byte("v1\n"))
	writeLenPrefixed(h, strings.Join(variants, ","))
	for _, m := range matches {
		writeLenPrefixed(h, m.Kind)
		writeLenPrefixed(h, m.Target)
		if m.Matched {
			h.Write([]byte{1})
		} else {
			h.Write([]byte{0})
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

// writeLenPrefixed writes a decimal length, a colon, and then the bytes of s
// to h. This encoding is injective: the length tells a hypothetical parser
// exactly how many bytes of s to read, so values that differ only in where
// their internal separators fall cannot collide.
func writeLenPrefixed(h io.Writer, s string) {
	fmt.Fprintf(h, "%d:", len(s))
	_, _ = h.Write([]byte(s))
}
