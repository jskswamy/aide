package seatbelt

import (
	"strings"
	"testing"
)

func TestRenderRules_Comment(t *testing.T) {
	rules := []Rule{SectionAllow("test section")}
	out := renderRules(rules)
	if !strings.Contains(out, ";; ") || !strings.Contains(out, "test section") {
		t.Errorf("expected comment, got %q", out)
	}
}

func TestRenderRules_Lines(t *testing.T) {
	block := "(deny file-write*\n    (require-not\n        (require-any\n            (subpath \"/tmp\"))))"
	rules := []Rule{AllowRule(block)}
	out := renderRules(rules)
	if !strings.Contains(out, block) {
		t.Errorf("expected rule block, got %q", out)
	}
}
