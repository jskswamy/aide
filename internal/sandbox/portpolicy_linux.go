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
	AllowSet    []int
	Mode        string
	Enforceable bool
}

// CommonPorts seeds the complement when only DenyPorts is configured.
// Port 0 is excluded.
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

	var allowSet []int

	switch {
	case len(allow) > 0 && len(deny) == 0:
		allowSet = validatePorts(allow)
		return PortPolicyEffective{
			AllowSet:    allowSet,
			Mode:        "allow_only",
			Enforceable: landlockABI4,
		}

	case len(allow) == 0 && len(deny) > 0:
		denySet := portSet(validatePorts(deny))
		for _, p := range CommonPorts {
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

// validatePorts drops out-of-range values. Callers needing a hard error use
// ValidatePortRange separately.
func validatePorts(ports []int) []int {
	out := make([]int, 0, len(ports))
	for _, p := range ports {
		if p >= 0 && p <= 65535 {
			out = append(out, p)
		}
	}
	return out
}

func portSet(ports []int) map[int]bool {
	m := make(map[int]bool, len(ports))
	for _, p := range ports {
		m[p] = true
	}
	return m
}

// ValidatePortRange returns an error if any port is outside the valid 0–65535 range.
func ValidatePortRange(ports []int) error {
	for _, p := range ports {
		if p < 0 || p > 65535 {
			return fmt.Errorf("invalid port %d: must be 0–65535", p)
		}
	}
	return nil
}
