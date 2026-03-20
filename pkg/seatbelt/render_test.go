package seatbelt

import (
	"strings"
	"testing"
)

func TestRenderRules_Comment(t *testing.T) {
	rules := []Rule{Comment("test section")}
	out := renderRules(rules)
	if !strings.Contains(out, ";; test section") {
		t.Errorf("expected comment, got %q", out)
	}
}

func TestRenderRules_Allow(t *testing.T) {
	rules := []Rule{Allow("process-exec")}
	out := renderRules(rules)
	if !strings.Contains(out, "(allow process-exec)") {
		t.Errorf("expected allow rule, got %q", out)
	}
}

func TestRenderRules_Raw(t *testing.T) {
	block := "(deny file-write*\n    (require-not\n        (require-any\n            (subpath \"/tmp\"))))"
	rules := []Rule{Raw(block)}
	out := renderRules(rules)
	if !strings.Contains(out, block) {
		t.Errorf("expected raw block, got %q", out)
	}
}
