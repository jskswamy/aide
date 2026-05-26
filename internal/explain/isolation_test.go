package explain_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoSecretResolutionImport asserts the explain package never imports the
// secrets package (which exposes DecryptSecretsFile / DiscoverAgeKey). This is
// the structural enforcement of T1: explain cannot resolve or decrypt secrets.
func TestNoSecretResolutionImport(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("reading package dir: %v", err)
	}
	fset := token.NewFileSet()
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Clean(name), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parsing %s: %v", name, err)
		}
		for _, imp := range f.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			if strings.HasSuffix(path, "/internal/secrets") {
				t.Errorf("%s imports %s — explain must never resolve secrets (T1)", name, path)
			}
		}
	}
}
