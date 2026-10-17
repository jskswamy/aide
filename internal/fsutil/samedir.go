package fsutil

import (
	"os"
	"runtime"
	"strings"
)

// caseInsensitiveDirs reports whether dir names that differ only by
// case name one dir, as on default macOS and Windows filesystems. On a
// case-sensitive macOS volume this treats a case-only difference
// between dirs that do not exist yet as one dir.
var caseInsensitiveDirs = runtime.GOOS == "darwin" || runtime.GOOS == "windows"

// SameDir reports whether a and b name one directory. Existing dirs
// are compared by identity (os.SameFile catches symlinks,
// case-insensitive filesystems and bind mounts); dirs that do not
// exist yet by their ResolveNearest paths, case-folded where the
// filesystem ignores case. An existing dir never matches a missing one.
func SameDir(a, b string) bool {
	ia, errA := os.Stat(a)
	ib, errB := os.Stat(b)
	switch {
	case errA == nil && errB == nil:
		return os.SameFile(ia, ib)
	case errA == nil || errB == nil:
		return false
	}
	ra, rb := ResolveNearest(a), ResolveNearest(b)
	if caseInsensitiveDirs {
		return strings.EqualFold(ra, rb)
	}
	return ra == rb
}
