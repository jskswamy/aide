package fsutil

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"syscall"
)

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

// CheckSymlinkCycle returns a non-nil error iff path traverses a symlink
// loop (filepath.EvalSymlinks reports ELOOP / "too many links"). Other
// EvalSymlinks errors — ENOENT for paths that don't exist yet, EACCES on
// unreadable intermediates — are tolerated and return nil; callers
// validating user-supplied paths at config load want a loud fail on
// cycles (the config is broken) but a silent pass on absent files (the
// path may be a not-yet-created cache dir).
//
// Path is used as-is; the caller is responsible for any home-expansion
// or normalization before invoking. Keeping path semantics out of this
// package preserves the convention that fsutil holds pure filesystem
// primitives.
func CheckSymlinkCycle(path string) error {
	_, err := filepath.EvalSymlinks(path)
	if IsSymlinkCycle(err) {
		return fmt.Errorf("symlink cycle detected at %q", path)
	}
	return nil
}

// IsSymlinkCycle reports whether err (typically from filepath.EvalSymlinks)
// indicates the kernel hit ELOOP traversing a symlink chain — i.e., the
// path is part of a loop. Returns false for nil errors, ENOENT, EACCES,
// and other non-loop conditions.
//
// Use this when you need to distinguish silent-fallback cases (missing
// path is fine, the file may not exist yet) from loud-fail cases
// (a cycle means the config is broken and the user needs to see the
// offending path).
func IsSymlinkCycle(err error) bool {
	if err == nil {
		return false
	}
	// Linux kernel surfaces ELOOP via syscall on the EvalSymlinks call.
	var errno syscall.Errno
	if errors.As(err, &errno) && errno == syscall.ELOOP {
		return true
	}
	// Go's path/filepath enforces its own max-link count (255) and
	// returns a plain errors.New error before the kernel's ELOOP fires
	// in practice. Match that sentinel by message: see Go source
	// src/path/filepath/symlink.go "EvalSymlinks: too many links".
	return strings.Contains(err.Error(), "too many links")
}
