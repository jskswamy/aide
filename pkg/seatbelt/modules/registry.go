// Guard registry for the seatbelt modules package.
//
// Provides lookup, filtering, and expansion for all built-in guards.

package modules

import (
	"github.com/jskswamy/aide/pkg/seatbelt"
)

// builtinGuards holds all registered guards in canonical order.
var builtinGuards []seatbelt.Guard

func init() {
	// always guards first
	builtinGuards = append(builtinGuards,
		BaseGuard(),
		SystemRuntimeGuard(),
		NetworkGuard(),
		FilesystemGuard(),
		KeychainGuard(),
		NodeToolchainGuard(),
		NixToolchainGuard(),
		GitIntegrationGuard(),
	)
	// default guards
	builtinGuards = append(builtinGuards,
		SSHKeysGuard(),
		CloudAWSGuard(),
		CloudGCPGuard(),
		CloudAzureGuard(),
		CloudDigitalOceanGuard(),
		CloudOCIGuard(),
		KubernetesGuard(),
		TerraformGuard(),
		VaultGuard(),
		BrowsersGuard(),
		PasswordManagersGuard(),
		AideSecretsGuard(),
	)
	// opt-in guards
	builtinGuards = append(builtinGuards,
		DockerGuard(),
		GithubCLIGuard(),
		NPMGuard(),
		NetrcGuard(),
		VercelGuard(),
	)
}

// AllGuards returns all built-in guards in registration order.
func AllGuards() []seatbelt.Guard {
	out := make([]seatbelt.Guard, len(builtinGuards))
	copy(out, builtinGuards)
	return out
}

// GuardByName returns the guard with the given name, or (nil, false) if not found.
func GuardByName(name string) (seatbelt.Guard, bool) {
	for _, g := range builtinGuards {
		if g.Name() == name {
			return g, true
		}
	}
	return nil, false
}

// GuardsByType returns all guards whose Type() equals typ.
func GuardsByType(typ string) []seatbelt.Guard {
	var out []seatbelt.Guard
	for _, g := range builtinGuards {
		if g.Type() == typ {
			out = append(out, g)
		}
	}
	return out
}

// cloudGuardNamesOnly returns just the five cloud provider guard names
// (excludes kubernetes, terraform, vault which are in CloudGuardNames()).
func cloudGuardNamesOnly() []string {
	return []string{
		"cloud-aws",
		"cloud-gcp",
		"cloud-azure",
		"cloud-digitalocean",
		"cloud-oci",
	}
}

// ExpandGuardName expands meta-guard names:
//   - "cloud"       → the 5 cloud provider guard names
//   - "all-default" → all guards with type "default"
//   - anything else → []string{name}
func ExpandGuardName(name string) []string {
	switch name {
	case "cloud":
		return cloudGuardNamesOnly()
	case "all-default":
		var names []string
		for _, g := range builtinGuards {
			if g.Type() == "default" {
				names = append(names, g.Name())
			}
		}
		return names
	default:
		return []string{name}
	}
}

// DefaultGuardNames returns the names of all "always" and "default" guards.
func DefaultGuardNames() []string {
	var names []string
	for _, g := range builtinGuards {
		if g.Type() == "always" || g.Type() == "default" {
			names = append(names, g.Name())
		}
	}
	return names
}

// typeOrder assigns a stable sort key to guard types.
func typeOrder(typ string) int {
	switch typ {
	case "always":
		return 0
	case "default":
		return 1
	case "opt-in":
		return 2
	default:
		return 3
	}
}

// ResolveActiveGuards looks up guards by name and returns them ordered by type
// (always → default → opt-in). Unknown names are silently skipped.
// Within each type bucket the original order of names is preserved (stable
// insertion sort).
func ResolveActiveGuards(names []string) []seatbelt.Guard {
	// buckets indexed by typeOrder value
	buckets := make([][]seatbelt.Guard, 4)

	seen := make(map[string]bool, len(names))
	for _, name := range names {
		if seen[name] {
			continue
		}
		g, ok := GuardByName(name)
		if !ok {
			continue
		}
		seen[name] = true
		idx := typeOrder(g.Type())
		buckets[idx] = append(buckets[idx], g)
	}

	var out []seatbelt.Guard
	for _, bucket := range buckets {
		out = append(out, bucket...)
	}
	return out
}
