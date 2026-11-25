package testutil

import (
	"flag"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

var updateGolden = flag.Bool("update", false, "rewrite golden files")

// goldenDir is fixed when the test binary starts, in the package
// directory, before any test changes directory.
var goldenDir = func() string {
	wd, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	return filepath.Join(wd, "testdata", "golden")
}()

// NormalizeGolden replaces each root, and its symlink-resolved form
// (macOS temp dirs live under /private), with its placeholder. Longer
// roots go first so a nested root is not split by its parent.
func NormalizeGolden(t *testing.T, s string, roots map[string]string) string {
	t.Helper()
	type pair struct{ from, to string }
	var pairs []pair
	for root, ph := range roots {
		root = strings.TrimSuffix(root, "/")
		pairs = append(pairs, pair{root, ph})
		if resolved, err := filepath.EvalSymlinks(root); err == nil && resolved != root {
			pairs = append(pairs, pair{resolved, ph})
		}
	}
	sort.Slice(pairs, func(i, j int) bool { return len(pairs[i].from) > len(pairs[j].from) })
	for _, p := range pairs {
		s = strings.ReplaceAll(s, p.from, p.to)
	}
	return s
}

// AssertGolden compares got with testdata/golden/<name> in the package
// under test, or rewrites the file when the test runs with -update.
func AssertGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join(goldenDir, name)
	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading golden (run with -update to create it): %v", err)
	}
	if string(want) != got {
		t.Errorf("%s differs from golden (run with -update to accept)\n--- want\n%s\n--- got\n%s", name, want, got)
	}
}
