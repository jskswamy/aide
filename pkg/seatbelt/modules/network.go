// Network module for macOS Seatbelt profiles.
//
// Controls network access with three modes: open, outbound-only, and none.
// Supports port-level filtering for outbound connections.

package modules

import (
	"fmt"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

// NetworkMode controls the level of network access.
type NetworkMode int

const (
	// NetworkOpen allows all network traffic (inbound + outbound).
	NetworkOpen NetworkMode = iota
	// NetworkOutbound allows outbound connections only.
	NetworkOutbound
	// NetworkNone denies all network traffic (default-deny covers it).
	NetworkNone
)

// PortOpts configures port-level filtering for outbound connections.
type PortOpts struct {
	// AllowPorts whitelists specific ports (deny all, then allow these).
	AllowPorts []int
	// DenyPorts blacklists specific ports.
	// Ignored if AllowPorts is set.
	DenyPorts []int
}

type networkModule struct {
	mode NetworkMode
	opts PortOpts
}

// Network returns a module that controls network access.
func Network(mode NetworkMode) seatbelt.Module {
	return &networkModule{mode: mode}
}

// NetworkWithPorts returns a network module with port-level filtering.
// Port filtering only applies to NetworkOutbound mode.
func NetworkWithPorts(mode NetworkMode, opts PortOpts) seatbelt.Module {
	return &networkModule{mode: mode, opts: opts}
}

func (m *networkModule) Name() string { return "Network" }

func (m *networkModule) Rules(_ *seatbelt.Context) []seatbelt.Rule {
	switch m.mode {
	case NetworkOpen:
		return []seatbelt.Rule{seatbelt.Allow("network*")}
	case NetworkOutbound:
		return m.outboundRules()
	case NetworkNone:
		return nil
	default:
		return nil
	}
}

func (m *networkModule) outboundRules() []seatbelt.Rule {
	// AllowPorts takes precedence over DenyPorts.
	if len(m.opts.AllowPorts) > 0 {
		return m.allowPortRules()
	}
	if len(m.opts.DenyPorts) > 0 {
		return m.denyPortRules()
	}
	return []seatbelt.Rule{seatbelt.Allow("network-outbound")}
}

func (m *networkModule) allowPortRules() []seatbelt.Rule {
	rules := []seatbelt.Rule{
		seatbelt.Deny("network-outbound"),
	}
	for _, port := range m.opts.AllowPorts {
		rules = append(rules,
			seatbelt.Raw(fmt.Sprintf(`(allow network-outbound (remote tcp "*:%d"))`, port)),
		)
		if port == 53 {
			rules = append(rules,
				seatbelt.Raw(fmt.Sprintf(`(allow network-outbound (remote udp "*:%d"))`, port)),
			)
		}
	}
	return rules
}

func (m *networkModule) denyPortRules() []seatbelt.Rule {
	rules := []seatbelt.Rule{
		seatbelt.Allow("network-outbound"),
	}
	for _, port := range m.opts.DenyPorts {
		rules = append(rules,
			seatbelt.Raw(fmt.Sprintf(`(deny network-outbound (remote tcp "*:%d"))`, port)),
		)
	}
	return rules
}
