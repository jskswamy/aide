// Package sliceutil holds generic slice helpers that were previously
// re-implemented per package. Stdlib-equivalent operations (Contains,
// Clone) are intentionally absent — callers should use slices.Contains
// and slices.Clone directly.
package sliceutil

// Dedup returns a new slice with duplicates removed in first-seen order.
// An empty or nil input returns nil so callers can compare against nil
// the same way the previous hand-rolled helpers behaved.
func Dedup[T comparable](s []T) []T {
	if len(s) == 0 {
		return nil
	}
	seen := make(map[T]struct{}, len(s))
	out := make([]T, 0, len(s))
	for _, v := range s {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}
