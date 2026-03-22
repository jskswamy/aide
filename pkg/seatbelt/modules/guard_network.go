// Network guard for macOS Seatbelt profiles.
//
// Controls network access with three modes: open, outbound-only, and none.
// Supports port-level filtering for outbound connections.

package modules

import (
	"fmt"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

// NetworkMode is an alias for seatbelt.NetworkMode for backward compatibility.
type NetworkMode = seatbelt.NetworkMode

// NetworkModeOpen, NetworkModeOutbound, NetworkModeNone are exported aliases for backward compat.
const (
	NetworkModeOpen     = seatbelt.NetworkOpen
	NetworkModeOutbound = seatbelt.NetworkOutbound
	NetworkModeNone     = seatbelt.NetworkNone
)

// NetworkOpen, NetworkOutbound, NetworkNone are exported aliases for backward compat.
const (
	NetworkOpen     = seatbelt.NetworkOpen
	NetworkOutbound = seatbelt.NetworkOutbound
	NetworkNone     = seatbelt.NetworkNone
)

// PortOpts configures port-level filtering for outbound connections.
type PortOpts struct {
	// AllowPorts whitelists specific ports (deny all, then allow these).
	AllowPorts []int
	// DenyPorts blacklists specific ports.
	// Ignored if AllowPorts is set.
	DenyPorts []int
}

// networkGuard reads network mode and port options from ctx fields.
type networkGuard struct{}

// NetworkGuard returns a Guard that reads ctx.Network, ctx.AllowPorts, ctx.DenyPorts.
func NetworkGuard() seatbelt.Guard { return &networkGuard{} }

func (g *networkGuard) Name() string        { return "network" }
func (g *networkGuard) Type() string        { return "always" }
func (g *networkGuard) Description() string { return "network access control (open/outbound/none)" }

func (g *networkGuard) Rules(ctx *seatbelt.Context) []seatbelt.Rule {
	if ctx == nil {
		return nil
	}
	switch ctx.Network {
	case seatbelt.NetworkOpen:
		return []seatbelt.Rule{seatbelt.Allow("network*")}
	case seatbelt.NetworkOutbound:
		return outboundRules(ctx.AllowPorts, ctx.DenyPorts)
	case seatbelt.NetworkNone:
		return nil
	default:
		return nil
	}
}

// networkModule is the backward-compat wrapper that stores mode and opts as constructor params.
type networkModule struct {
	mode seatbelt.NetworkMode
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
	case seatbelt.NetworkOpen:
		return []seatbelt.Rule{seatbelt.Allow("network*")}
	case seatbelt.NetworkOutbound:
		return outboundRules(m.opts.AllowPorts, m.opts.DenyPorts)
	case seatbelt.NetworkNone:
		return nil
	default:
		return nil
	}
}

func outboundRules(allowPorts, denyPorts []int) []seatbelt.Rule {
	if len(allowPorts) > 0 {
		return allowPortRules(allowPorts)
	}
	if len(denyPorts) > 0 {
		return denyPortRules(denyPorts)
	}
	return []seatbelt.Rule{seatbelt.Allow("network-outbound")}
}

func allowPortRules(ports []int) []seatbelt.Rule {
	rules := []seatbelt.Rule{
		seatbelt.Deny("network-outbound"),
	}
	for _, port := range ports {
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

func denyPortRules(ports []int) []seatbelt.Rule {
	rules := []seatbelt.Rule{
		seatbelt.Allow("network-outbound"),
	}
	for _, port := range ports {
		rules = append(rules,
			seatbelt.Raw(fmt.Sprintf(`(deny network-outbound (remote tcp "*:%d"))`, port)),
		)
	}
	return rules
}
