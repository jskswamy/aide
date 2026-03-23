package guards_test

import (
	"strings"
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt"
	"github.com/jskswamy/aide/pkg/seatbelt/guards"
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
	m := guards.Base()
	if m.Name() != "base" {
		t.Errorf("expected Name() = %q, got %q", "base", m.Name())
	}

	output := renderTestRules(m.Rules(nil))

	if !strings.Contains(output, "(version 1)") {
		t.Error("expected output to contain (version 1)")
	}
	if !strings.Contains(output, "(deny default)") {
		t.Error("expected output to contain (deny default)")
	}
}

func TestGuard_Base(t *testing.T) {
	g := guards.BaseGuard()

	if g.Name() != "base" {
		t.Errorf("expected Name() = %q, got %q", "base", g.Name())
	}
	if g.Type() != "always" {
		t.Errorf("expected Type() = %q, got %q", "always", g.Type())
	}
	if g.Description() != "(version 1), (deny default)" {
		t.Errorf("expected Description() = %q, got %q", "(version 1), (deny default)", g.Description())
	}

	output := renderTestRules(g.Rules(nil))
	if !strings.Contains(output, "(version 1)") {
		t.Error("expected rules to contain (version 1)")
	}
	if !strings.Contains(output, "(deny default)") {
		t.Error("expected rules to contain (deny default)")
	}
}
