package fsutil

import "path/filepath"

// ResolveOrSelf returns filepath.EvalSymlinks(path) when resolution
// succeeds, otherwise the input path unchanged. The fallback covers
// the common case of writing to a path that does not exist yet
// (ENOENT) — callers want a usable target for the upcoming create,
// not an error.
//
// This idiom is the foundation of all symlink-aware filesystem work in
// the repo: atomic writes resolve before computing the rename target,
// sandbox rule generators resolve before emitting allow rules, and
// guards resolve before emitting deny rules. Single-sourcing it here
// keeps the contract consistent.
func ResolveOrSelf(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return path
}
