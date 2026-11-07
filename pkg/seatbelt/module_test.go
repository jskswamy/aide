package seatbelt

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestNoInternalImports(t *testing.T) {
	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		f, parseErr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if parseErr != nil {
			return parseErr
		}
		for _, imp := range f.Imports {
			pkgPath, _ := strconv.Unquote(imp.Path.Value)
			if strings.HasPrefix(pkgPath, "github.com/jskswamy/aide/internal/") {
				t.Errorf("public pkg/seatbelt file %s imports internal package %q", path, pkgPath)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking pkg/seatbelt: %v", err)
	}
}


func TestRuleIntent_AllowRule(t *testing.T) {
	r := AllowRule("(allow file-read* (subpath \"/usr\"))")
	if r.Intent() != Allow {
		t.Errorf("expected Allow (%d), got %d", Allow, r.Intent())
	}
	if r.String() != "(allow file-read* (subpath \"/usr\"))" {
		t.Errorf("unexpected content: %q", r.String())
	}
}

func TestRuleIntent_DenyRule(t *testing.T) {
	r := DenyRule(`(deny file-read-data (subpath "/home/.ssh"))`)
	if r.Intent() != Deny {
		t.Errorf("expected Deny (%d), got %d", Deny, r.Intent())
	}
	if r.String() != `(deny file-read-data (subpath "/home/.ssh"))` {
		t.Errorf("unexpected content: %q", r.String())
	}
}

func TestRuleIntent_SectionAllow(t *testing.T) {
	r := SectionAllow("Infrastructure")
	if r.Intent() != Allow {
		t.Errorf("expected Allow (%d), got %d", Allow, r.Intent())
	}
	if r.String() != ";; --- Infrastructure ---\n" {
		t.Errorf("unexpected content: %q", r.String())
	}
}

func TestRuleIntent_SectionDeny(t *testing.T) {
	r := SectionDeny("Credentials")
	if r.Intent() != Deny {
		t.Errorf("expected Deny (%d), got %d", Deny, r.Intent())
	}
	if r.String() != ";; --- Credentials ---\n" {
		t.Errorf("unexpected content: %q", r.String())
	}
}

func TestRuleIntent_UtilityConstructors(t *testing.T) {
	tests := []struct {
		name string
		rule Rule
	}{
		{"AllowRule", AllowRule("test")},
		{"SectionAllow", SectionAllow("test")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.rule.Intent() != Allow {
				t.Errorf("expected Allow (%d), got %d", Allow, tt.rule.Intent())
			}
		})
	}
}
