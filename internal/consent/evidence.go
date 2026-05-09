// Package consent stores user approvals of auto-detected toolchain
// variant selections. It is a sibling aggregate to the trust package
// within the User Approval bounded context. Storage mechanics are
// reused via internal/approvalstore.
package consent

import (
	"sort"
	"strings"

	"github.com/jskswamy/aide/internal/hashutil"
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

	b := hashutil.New("v1").Field(strings.Join(variants, ","))
	for _, m := range matches {
		b.Field(m.Kind).Field(m.Target)
		if m.Matched {
			b.Bytes([]byte{1})
		} else {
			b.Bytes([]byte{0})
		}
	}
	return b.HexSum()
}
