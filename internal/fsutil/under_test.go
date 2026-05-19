package fsutil_test

import (
	"path/filepath"
	"testing"

	"github.com/jskswamy/aide/internal/fsutil"
)

// TestIsUnderDir pins the inclusive-prefix contract used to gate sandbox
// widening, atomic-write tmp+rename siblings, and prefix-closed sets. The
// helper accepts equality (path == dir) — callers that need a strict
// "child of" predicate must add their own path != dir guard explicitly.
// Sub-tests cover every edge that has bitten this codebase before.
func TestIsUnderDir(t *testing.T) {
	sep := string(filepath.Separator)
	tests := []struct {
		name string
		path string
		dir  string
		want bool
	}{
		{"equal paths are under themselves", "/home/user", "/home/user", true},
		{"strict child", "/home/user/x", "/home/user", true},
		{"deep child", "/home/user/a/b/c", "/home/user", true},
		{"similar prefix is NOT under", "/home/user-other", "/home/user", false},
		{"similar prefix two-component is NOT under", "/home/user-other/x", "/home/user", false},
		{"reverse: dir under path is NOT under", "/home", "/home/user", false},
		{"disjoint paths", "/etc", "/home/user", false},
		{"empty dir", "/home/user", "", false},
		{"empty path", "", "/home/user", false},
		{"both empty", "", "", true},
		{"trailing separator on dir matches", "/home/user/x", "/home/user" + sep, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fsutil.IsUnderDir(tt.path, tt.dir); got != tt.want {
				t.Errorf("IsUnderDir(%q, %q) = %v, want %v", tt.path, tt.dir, got, tt.want)
			}
		})
	}
}
