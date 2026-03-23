// Network guard for macOS Seatbelt profiles.
//
// Controls network access with three modes: open, outbound-only, and none.
// Supports port-level filtering for outbound connections.

package guards

import (
	"fmt"

	"github.com/jskswamy/aide/pkg/seatbelt"
)

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
	case "outbound":
		return networkOutboundRules(ctx.AllowPorts, ctx.DenyPorts)
	case "none":
		return nil
	case "unrestricted", "":
		return []seatbelt.Rule{seatbelt.Allow("network*")}
	default:
		return nil
	}
}

func networkOutboundRules(allowPorts, denyPorts []int) []seatbelt.Rule {
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
