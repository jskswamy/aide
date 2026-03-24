package guards_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt"
	"github.com/jskswamy/aide/pkg/seatbelt/guards"
)

func TestDenyDir(t *testing.T) {
	rules := guards.DenyDir("/home/user/.ssh")
	if len(rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(rules))
	}
	output := rules[0].String() + rules[1].String()
	if !strings.Contains(output, `(subpath "/home/user/.ssh")`) {
		t.Error("DenyDir should use subpath")
	}
}

func TestDenyFile(t *testing.T) {
	rules := guards.DenyFile("/home/user/.vault-token")
	if len(rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(rules))
	}
	output := rules[0].String() + rules[1].String()
	if !strings.Contains(output, `(literal "/home/user/.vault-token")`) {
		t.Error("DenyFile should use literal")
	}
}

func TestDenyDir_Intent(t *testing.T) {
	rules := guards.DenyDir("/home/.ssh")
	for _, r := range rules {
		if r.Intent() != seatbelt.Deny {
			t.Errorf("DenyDir should produce Deny intent, got %d", r.Intent())
		}
	}
}

func TestDenyFile_Intent(t *testing.T) {
	rules := guards.DenyFile("/home/.vault-token")
	for _, r := range rules {
		if r.Intent() != seatbelt.Deny {
			t.Errorf("DenyFile should produce Deny intent, got %d", r.Intent())
		}
	}
}

func TestAllowReadFile_Intent(t *testing.T) {
	r := guards.AllowReadFile("/home/.ssh/known_hosts")
	if r.Intent() != seatbelt.Allow {
		t.Errorf("AllowReadFile should produce Allow intent, got %d", r.Intent())
	}
}

func TestSplitColonPaths_EmptySegments(t *testing.T) {
	result := guards.SplitColonPaths("/a::/b:")
	if len(result) != 2 || result[0] != "/a" || result[1] != "/b" {
		t.Errorf("expected [/a, /b], got %v", result)
	}
}

func TestDirExists(t *testing.T) {
	t.Run("true for existing directory", func(t *testing.T) {
		dir := t.TempDir()
		if !guards.TestDirExists(dir) {
			t.Error("dirExists should return true for existing directory")
		}
	})
	t.Run("false for nonexistent path", func(t *testing.T) {
		if guards.TestDirExists("/nonexistent/path/that/does/not/exist") {
			t.Error("dirExists should return false for nonexistent path")
		}
	})
	t.Run("false for file", func(t *testing.T) {
		dir := t.TempDir()
		f := filepath.Join(dir, "afile.txt")
		if err := os.WriteFile(f, []byte("hello"), 0o644); err != nil {
			t.Fatal(err)
		}
		if guards.TestDirExists(f) {
			t.Error("dirExists should return false for a file")
		}
	})
}

func TestPathExists(t *testing.T) {
	t.Run("true for existing file", func(t *testing.T) {
		dir := t.TempDir()
		f := filepath.Join(dir, "afile.txt")
		if err := os.WriteFile(f, []byte("hello"), 0o644); err != nil {
			t.Fatal(err)
		}
		if !guards.TestPathExists(f) {
			t.Error("pathExists should return true for existing file")
		}
	})
	t.Run("false for nonexistent path", func(t *testing.T) {
		if guards.TestPathExists("/nonexistent/path/that/does/not/exist") {
			t.Error("pathExists should return false for nonexistent path")
		}
	})
}
