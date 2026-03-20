package modules_test

import (
	"strings"
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt"
	"github.com/jskswamy/aide/pkg/seatbelt/modules"
)

func renderTestRules(rules []seatbelt.Rule) string {
	var b strings.Builder
	for _, r := range rules {
		b.WriteString(r.String())
		b.WriteByte('\n')
	}
	return b.String()
}

func TestBase_DenyDefault(t *testing.T) {
	m := modules.Base()
	if m.Name() != "Base" {
		t.Errorf("expected Name() = %q, got %q", "Base", m.Name())
	}

	output := renderTestRules(m.Rules(nil))

	if !strings.Contains(output, "(version 1)") {
		t.Error("expected output to contain (version 1)")
	}
	if !strings.Contains(output, "(deny default)") {
		t.Error("expected output to contain (deny default)")
	}
}
