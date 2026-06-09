//go:build linux

// Package sandbox resolves AllowPorts / DenyPorts policy to a concrete TCP allow-set.
// Landlock ABI ≥ 4 is required to actually enforce port filtering.
package sandbox

import "fmt"

// PortPolicyEffective is the resolved port enforcement descriptor.
// Mode is one of: "unrestricted", "allow_only", "deny_complement",
// "allow_intersect_deny". Enforceable is false when the backend cannot honour
// AllowSet — IsolationTier must be degraded in that case.
type PortPolicyEffective struct {
	AllowSet    []uint16 // all values guaranteed to be in [1, 65535]
	Mode        string
	Enforceable bool
}

// CommonPorts is used for documentation and status display to show
// representative well-known ports. It is NOT used for deny-list complement
// computation — see DerivePortPolicy for the full-range complement semantics.
var CommonPorts = []int{
	22,    // SSH
	53,    // DNS (UDP handled by OS; TCP fallback)
	80,    // HTTP
	443,   // HTTPS
	5173,  // Vite dev server
	8080,  // HTTP alt
	8443,  // HTTPS alt
	8888,  // HTTP alt
	9000,  // generic dev server
	3000,  // Node.js / React dev server
	4000,  // Phoenix / misc
	5000,  // Flask / misc
	5432,  // PostgreSQL
	3306,  // MySQL
	27017, // MongoDB
	6379,  // Redis
	9090,  // Python dev server
	2375,  // Docker daemon (unencrypted)
	2376,  // Docker daemon (TLS)
}

// DerivePortPolicy computes the effective TCP port allow-set from the policy.
// landlockABI4 must be true for the result to be enforceable.
func DerivePortPolicy(policy Policy, landlockABI4 bool) PortPolicyEffective {
	allow := policy.AllowPorts
	deny := policy.DenyPorts

	if len(allow) == 0 && len(deny) == 0 {
		return PortPolicyEffective{
			Mode:        "unrestricted",
			Enforceable: true, // nothing to enforce
		}
	}

	var allowSet []uint16

	switch {
	case len(allow) > 0 && len(deny) == 0:
		return PortPolicyEffective{
			AllowSet:    validatePorts(allow),
			Mode:        "allow_only",
			Enforceable: landlockABI4,
		}

	case len(allow) == 0 && len(deny) > 0:
		// Allow the full TCP port range (1–65535) minus the denied ports.
		// Using only CommonPorts as the complement seed would silently block
		// legitimate ports (e.g. 5173, 8888) that are absent from that list.
		denySet := portSet(validatePorts(deny))
		for i := 1; i <= 65535; i++ {
			p := uint16(i) //nolint:gosec // i ∈ [1,65535] by loop bounds
			if !denySet[p] {
				allowSet = append(allowSet, p)
			}
		}
		return PortPolicyEffective{
			AllowSet:    allowSet,
			Mode:        "deny_complement",
			Enforceable: landlockABI4,
		}

	default: // both allow and deny set: allow ∩ ¬deny
		denySet := portSet(validatePorts(deny))
		for _, p := range validatePorts(allow) {
			if !denySet[p] {
				allowSet = append(allowSet, p)
			}
		}
		return PortPolicyEffective{
			AllowSet:    allowSet,
			Mode:        "allow_intersect_deny",
			Enforceable: landlockABI4,
		}
	}
}

// validatePorts drops out-of-range values and returns a []uint16 whose values
// are guaranteed to be in [1, 65535]. Port 0 is excluded: it is not a valid
// TCP connect/bind destination, and Landlock treats ConnectTCP(0) as a
// wildcard that allows every port — silently broadening "allow_only" while
// the deny_complement loop (1–65535) silently excludes it. Rejecting it at
// entry keeps both modes symmetric. Callers needing a hard error use
// ValidatePortRange separately.
func validatePorts(ports []int) []uint16 {
	out := make([]uint16, 0, len(ports))
	for _, p := range ports {
		if p >= 1 && p <= 65535 {
			out = append(out, uint16(p)) //nolint:gosec // bounds checked above
		}
	}
	return out
}

func portSet(ports []uint16) map[uint16]bool {
	m := make(map[uint16]bool, len(ports))
	for _, p := range ports {
		m[p] = true
	}
	return m
}

// ValidatePortRange returns an error if any port is outside the valid 1–65535
// range. Port 0 is rejected: see validatePorts for the Landlock-wildcard
// rationale.
func ValidatePortRange(ports []int) error {
	for _, p := range ports {
		if p < 1 || p > 65535 {
			return fmt.Errorf("invalid port %d: must be 1–65535", p)
		}
	}
	return nil
}
