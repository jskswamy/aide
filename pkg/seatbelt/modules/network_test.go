package modules_test

import (
	"strings"
	"testing"

	"github.com/jskswamy/aide/pkg/seatbelt/modules"
)

func TestNetwork_Open(t *testing.T) {
	m := modules.Network(modules.NetworkOpen)
	if m.Name() != "Network" {
		t.Errorf("expected Name() = %q, got %q", "Network", m.Name())
	}

	output := renderTestRules(m.Rules(nil))

	if !strings.Contains(output, "(allow network*)") {
		t.Error("expected output to contain (allow network*)")
	}
}

func TestNetwork_Outbound(t *testing.T) {
	m := modules.Network(modules.NetworkOutbound)
	output := renderTestRules(m.Rules(nil))

	if !strings.Contains(output, "(allow network-outbound)") {
		t.Error("expected output to contain (allow network-outbound)")
	}
	if strings.Contains(output, "(allow network*)") {
		t.Error("NetworkOutbound should not contain (allow network*)")
	}
}

func TestNetwork_None(t *testing.T) {
	m := modules.Network(modules.NetworkNone)
	rules := m.Rules(nil)

	if len(rules) != 0 {
		t.Errorf("expected no rules for NetworkNone, got %d", len(rules))
	}
}

func TestNetworkWithPorts_AllowPorts(t *testing.T) {
	m := modules.NetworkWithPorts(modules.NetworkOutbound, modules.PortOpts{
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
	m := modules.NetworkWithPorts(modules.NetworkOutbound, modules.PortOpts{
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
	m := modules.NetworkWithPorts(modules.NetworkOutbound, modules.PortOpts{
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
	m := modules.NetworkWithPorts(modules.NetworkOpen, modules.PortOpts{
		AllowPorts: []int{443},
	})
	output := renderTestRules(m.Rules(nil))

	// Open mode should just emit (allow network*), ignoring port opts
	if !strings.Contains(output, "(allow network*)") {
		t.Error("expected (allow network*) for open mode")
	}
}
