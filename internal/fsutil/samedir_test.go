package fsutil_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jskswamy/aide/internal/fsutil"
)

func TestSameDir(t *testing.T) {
	root := t.TempDir()
	dir, link := filepath.Join(root, "real"), filepath.Join(root, "link")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		a, b string
		want bool
	}{
		{"existing dir through a symlink", dir, link, true},
		{"unclean spelling", dir, dir + "/./", true},
		{"missing dir under a symlinked parent", filepath.Join(dir, "new"), filepath.Join(link, "new"), true},
		{"different missing dirs", filepath.Join(root, "x"), filepath.Join(root, "y"), false},
		{"existing and missing", dir, filepath.Join(root, "missing"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := fsutil.SameDir(tc.a, tc.b); got != tc.want {
				t.Errorf("SameDir(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

// Before either dir exists there is no inode to compare, so names that
// differ only by case are one dir where the filesystem ignores case.
func TestSameDir_CaseOnlyDifferenceOfMissingDirs(t *testing.T) {
	root := t.TempDir()
	a, b := filepath.Join(root, "work"), filepath.Join(root, "Work")
	for _, fold := range []bool{true, false} {
		t.Cleanup(fsutil.SetCaseInsensitiveDirs(fold))
		if got := fsutil.SameDir(a, b); got != fold {
			t.Errorf("case-insensitive=%v: SameDir = %v, want %v", fold, got, fold)
		}
	}
}
