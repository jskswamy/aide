package guards_test

import (
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
		if r.Intent() != seatbelt.Restrict {
			t.Errorf("DenyDir should produce Restrict intent, got %d", r.Intent())
		}
	}
}

func TestDenyFile_Intent(t *testing.T) {
	rules := guards.DenyFile("/home/.vault-token")
	for _, r := range rules {
		if r.Intent() != seatbelt.Restrict {
			t.Errorf("DenyFile should produce Restrict intent, got %d", r.Intent())
		}
	}
}

func TestAllowReadFile_Intent(t *testing.T) {
	r := guards.AllowReadFile("/home/.ssh/known_hosts")
	if r.Intent() != seatbelt.Grant {
		t.Errorf("AllowReadFile should produce Grant intent, got %d", r.Intent())
	}
}

func TestSplitColonPaths_EmptySegments(t *testing.T) {
	result := guards.SplitColonPaths("/a::/b:")
	if len(result) != 2 || result[0] != "/a" || result[1] != "/b" {
		t.Errorf("expected [/a, /b], got %v", result)
	}
}
