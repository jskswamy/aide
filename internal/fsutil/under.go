package fsutil

import (
	"path/filepath"
	"strings"
)

// IsUnderDir reports whether path is dir itself or nested below it,
// using path-separator-aware comparison so that, e.g., "/home/user-other"
// is NOT considered under "/home/user". Inputs are filepath.Clean'd
// before comparison so trailing separators and redundant "/." segments
// don't affect the result.
//
// Semantics are INCLUSIVE: IsUnderDir("/x", "/x") returns true. Callers
// that need a strict "child of" predicate (e.g., refusing to widen a
// sandbox rule to $HOME itself) must add an explicit "path != dir"
// guard alongside the call — the strict variant is rare enough that
// burying it inside this helper would be a footgun.
//
// Both-empty inputs return true (consistent with the equality rule for
// any pair of equal paths). Any other case where one input is empty
// returns false (filepath.Clean turns "" into ".", which never matches
// an absolute or non-trivial relative path).
func IsUnderDir(path, dir string) bool {
	p := filepath.Clean(path)
	d := filepath.Clean(dir)
	if p == d {
		return true
	}
	return strings.HasPrefix(p, d+string(filepath.Separator))
}
