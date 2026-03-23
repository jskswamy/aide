package guards_test

import (
	"strings"
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt"
	"github.com/jskswamy/aide/pkg/seatbelt/guards"
)

func TestNetwork_Open(t *testing.T) {
	m := guards.Network(guards.NetworkOpen)
	if m.Name() != "Network" {
		t.Errorf("expected Name() = %q, got %q", "Network", m.Name())
	}

	output := renderTestRules(m.Rules(nil))

	if !strings.Contains(output, "(allow network*)") {
		t.Error("expected output to contain (allow network*)")
	}
}

func TestNetwork_Outbound(t *testing.T) {
	m := guards.Network(guards.NetworkOutbound)
	output := renderTestRules(m.Rules(nil))

	if !strings.Contains(output, "(allow network-outbound)") {
		t.Error("expected output to contain (allow network-outbound)")
	}
	if strings.Contains(output, "(allow network*)") {
		t.Error("NetworkOutbound should not contain (allow network*)")
	}
}

func TestNetwork_None(t *testing.T) {
	m := guards.Network(guards.NetworkNone)
	rules := m.Rules(nil)

	if len(rules) != 0 {
		t.Errorf("expected no rules for NetworkNone, got %d", len(rules))
	}
}

func TestNetworkWithPorts_AllowPorts(t *testing.T) {
	m := guards.NetworkWithPorts(guards.NetworkOutbound, guards.PortOpts{
		AllowPorts: []int{443, 53, 80},
	})
	output := renderTestRules(m.Rules(nil))

	// Should deny all outbound first
	if !strings.Contains(output, "(deny network-outbound)") {
		t.Error("expected deny network-outbound before allow rules")
	}

	// Should allow specific TCP ports
	if !strings.Contains(output, `(allow network-outbound (remote tcp "*:443"))`) {
		t.Error("expected allow for TCP port 443")
	}
	if !strings.Contains(output, `(allow network-outbound (remote tcp "*:80"))`) {
		t.Error("expected allow for TCP port 80")
	}
	if !strings.Contains(output, `(allow network-outbound (remote tcp "*:53"))`) {
		t.Error("expected allow for TCP port 53")
	}

	// Port 53 should also get a UDP rule
	if !strings.Contains(output, `(allow network-outbound (remote udp "*:53"))`) {
		t.Error("expected allow for UDP port 53 (DNS)")
	}
}

func TestNetworkWithPorts_DenyPorts(t *testing.T) {
	m := guards.NetworkWithPorts(guards.NetworkOutbound, guards.PortOpts{
		DenyPorts: []int{22, 25},
	})
	output := renderTestRules(m.Rules(nil))

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

func TestNetworkWithPorts_AllowTakesPrecedence(t *testing.T) {
	m := guards.NetworkWithPorts(guards.NetworkOutbound, guards.PortOpts{
		AllowPorts: []int{443},
		DenyPorts:  []int{22}, // should be ignored
	})
	output := renderTestRules(m.Rules(nil))

	if !strings.Contains(output, `(allow network-outbound (remote tcp "*:443"))`) {
		t.Error("expected allow for TCP port 443")
	}
	// DenyPorts should be ignored when AllowPorts is set
	if strings.Contains(output, `(deny network-outbound (remote tcp "*:22"))`) {
		t.Error("DenyPorts should be ignored when AllowPorts is set")
	}
}

func TestNetworkWithPorts_OpenModeIgnoresPorts(t *testing.T) {
	m := guards.NetworkWithPorts(guards.NetworkOpen, guards.PortOpts{
		AllowPorts: []int{443},
	})
	output := renderTestRules(m.Rules(nil))

	// Open mode should just emit (allow network*), ignoring port opts
	if !strings.Contains(output, "(allow network*)") {
		t.Error("expected (allow network*) for open mode")
	}
}

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

func TestGuard_Network_Open(t *testing.T) {
	g := guards.NetworkGuard()
	ctx := &seatbelt.Context{Network: guards.NetworkOpen}
	output := renderTestRules(g.Rules(ctx))

	if !strings.Contains(output, "(allow network*)") {
		t.Error("expected (allow network*) for NetworkOpen")
	}
}

func TestGuard_Network_Outbound(t *testing.T) {
	g := guards.NetworkGuard()
	ctx := &seatbelt.Context{Network: guards.NetworkOutbound}
	output := renderTestRules(g.Rules(ctx))

	if !strings.Contains(output, "(allow network-outbound)") {
		t.Error("expected (allow network-outbound) for NetworkOutbound")
	}
}

func TestGuard_Network_None(t *testing.T) {
	g := guards.NetworkGuard()
	ctx := &seatbelt.Context{Network: guards.NetworkNone}
	rules := g.Rules(ctx)

	if len(rules) != 0 {
		t.Errorf("expected no rules for NetworkNone, got %d", len(rules))
	}
}

func TestGuard_Network_AllowPorts(t *testing.T) {
	g := guards.NetworkGuard()
	ctx := &seatbelt.Context{
		Network:    guards.NetworkOutbound,
		AllowPorts: []int{443, 53},
	}
	output := renderTestRules(g.Rules(ctx))

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
