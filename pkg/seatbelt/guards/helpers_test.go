package guards_test

import (
	"strings"
	"testing"

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

func TestSplitColonPaths_EmptySegments(t *testing.T) {
	result := guards.SplitColonPaths("/a::/b:")
	if len(result) != 2 || result[0] != "/a" || result[1] != "/b" {
		t.Errorf("expected [/a, /b], got %v", result)
	}
}
