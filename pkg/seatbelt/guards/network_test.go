package guards_test

import (
	"strings"
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt"
	"github.com/jskswamy/aide/pkg/seatbelt/guards"
)

func TestGuard_Network_Metadata(t *testing.T) {
	g := guards.NetworkGuard()

	if g.Name() != "network" {
		t.Errorf("expected Name() = %q, got %q", "network", g.Name())
	}
	if g.Type() != "always" {
		t.Errorf("expected Type() = %q, got %q", "always", g.Type())
	}
	if g.Description() == "" {
		t.Error("expected non-empty Description()")
	}
}

func TestGuard_Network_Unrestricted(t *testing.T) {
	g := guards.NetworkGuard()
	ctx := &seatbelt.Context{Network: "unrestricted"}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	if !strings.Contains(output, "(allow network*)") {
		t.Error("expected (allow network*) for unrestricted")
	}
}

func TestGuard_Network_Empty(t *testing.T) {
	g := guards.NetworkGuard()
	ctx := &seatbelt.Context{Network: ""}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	if !strings.Contains(output, "(allow network*)") {
		t.Error("expected (allow network*) for empty network (default unrestricted)")
	}
}

func TestGuard_Network_Outbound(t *testing.T) {
	g := guards.NetworkGuard()
	ctx := &seatbelt.Context{Network: "outbound"}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	if !strings.Contains(output, "(allow network-outbound)") {
		t.Error("expected (allow network-outbound) for outbound")
	}
}

func TestGuard_Network_None(t *testing.T) {
	g := guards.NetworkGuard()
	ctx := &seatbelt.Context{Network: "none"}
	result := g.Rules(ctx)
	rules := result.Rules

	if len(rules) != 0 {
		t.Errorf("expected no rules for none, got %d", len(rules))
	}
}

func TestGuard_Network_AllowPorts(t *testing.T) {
	g := guards.NetworkGuard()
	ctx := &seatbelt.Context{
		Network:    "outbound",
		AllowPorts: []int{443, 53},
	}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	if !strings.Contains(output, "(deny network-outbound)") {
		t.Error("expected deny network-outbound before allow rules")
	}
	if !strings.Contains(output, `(allow network-outbound (remote tcp "*:443"))`) {
		t.Error("expected allow for TCP port 443")
	}
	if !strings.Contains(output, `(allow network-outbound (remote udp "*:53"))`) {
		t.Error("expected allow for UDP port 53 (DNS)")
	}
}

func TestGuard_Network_DenyPorts(t *testing.T) {
	g := guards.NetworkGuard()
	ctx := &seatbelt.Context{
		Network:   "outbound",
		DenyPorts: []int{22, 25},
	}
	result := g.Rules(ctx)
	output := renderTestRules(result.Rules)

	if !strings.Contains(output, `(deny network-outbound (remote tcp "*:22"))`) {
		t.Error("expected deny for TCP port 22")
	}
	if !strings.Contains(output, `(deny network-outbound (remote tcp "*:25"))`) {
		t.Error("expected deny for TCP port 25")
	}

	// Should still have the base outbound allow
	if !strings.Contains(output, "(allow network-outbound)") {
		t.Error("expected allow network-outbound with deny ports")
	}
}

func TestGuard_Network_NilContext(t *testing.T) {
	g := guards.NetworkGuard()
	result := g.Rules(nil)
	rules := result.Rules

	if rules != nil {
		t.Errorf("expected nil rules for nil context, got %v", rules)
	}
}
