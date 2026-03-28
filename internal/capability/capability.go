package capability

import (
	"fmt"

	"github.com/jskswamy/aide/internal/config"
)

// Capability defines a task-oriented permission bundle.
type Capability struct {
	Name        string
	Description string
	Extends     string   // single parent inheritance
	Combines    []string // merge multiple capabilities
	Unguard     []string
	Readable    []string
	Writable    []string
	Deny        []string
	EnvAllow    []string
	EnableGuard []string
}

// ResolvedCapability is the flattened result after inheritance resolution.
type ResolvedCapability struct {
	Name        string
	Sources     []string // trace: ["k8s-dev", "k8s"]
	Unguard     []string
	Readable    []string
	Writable    []string
	Deny        []string
	EnvAllow    []string
	EnableGuard []string
}

// Set is the merged result of multiple activated capabilities.
type Set struct {
	Capabilities  []ResolvedCapability
	NeverAllow    []string
	NeverAllowEnv []string
}

// SandboxOverrides is an alias for config.SandboxOverrides.
// The canonical definition lives in config to avoid circular imports
// between capability and sandbox packages.
type SandboxOverrides = config.SandboxOverrides

const maxDepth = 10

// ResolveOne resolves a single capability by name, walking extends/combines chains.
func ResolveOne(name string, registry map[string]Capability) (*ResolvedCapability, error) {
	return resolveOne(name, registry, make(map[string]bool), 0)
}

func resolveOne(name string, registry map[string]Capability, visited map[string]bool, depth int) (*ResolvedCapability, error) {
	if depth > maxDepth {
		return nil, fmt.Errorf("capability inheritance depth exceeds %d for %q", maxDepth, name)
	}
	if visited[name] {
		return nil, fmt.Errorf("circular capability reference: %q", name)
	}
	visited[name] = true

	entry, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("unknown capability: %q", name)
	}

	if entry.Extends != "" && len(entry.Combines) > 0 {
		return nil, fmt.Errorf("capability %q: extends and combines are mutually exclusive", name)
	}

	if entry.Extends != "" {
		parent, err := resolveOne(entry.Extends, registry, visited, depth+1)
		if err != nil {
			return nil, fmt.Errorf("resolving parent of %q: %w", name, err)
		}
		return mergeChild(parent, &entry), nil
	}

	if len(entry.Combines) > 0 {
		result := &ResolvedCapability{Name: name, Sources: []string{name}}
		for _, combineName := range entry.Combines {
			resolved, err := resolveOne(combineName, registry, copyVisited(visited), depth+1)
			if err != nil {
				return nil, fmt.Errorf("resolving combined %q in %q: %w", combineName, name, err)
			}
			result = mergeAdditive(result, resolved)
		}
		// Apply local overrides on top
		result = mergeChild(result, &entry)
		result.Name = name
		return result, nil
	}

	// Base case — no extends, no combines
	return flatten(&entry), nil
}

func flatten(capDef *Capability) *ResolvedCapability {
	return &ResolvedCapability{
		Name:        capDef.Name,
		Sources:     []string{capDef.Name},
		Unguard:     copyStrings(capDef.Unguard),
		Readable:    copyStrings(capDef.Readable),
		Writable:    copyStrings(capDef.Writable),
		Deny:        copyStrings(capDef.Deny),
		EnvAllow:    copyStrings(capDef.EnvAllow),
		EnableGuard: copyStrings(capDef.EnableGuard),
	}
}

func mergeChild(parent *ResolvedCapability, child *Capability) *ResolvedCapability {
	return &ResolvedCapability{
		Name:        child.Name,
		Sources:     append([]string{child.Name}, parent.Sources...),
		Unguard:     dedup(append(parent.Unguard, child.Unguard...)),
		Readable:    dedup(append(parent.Readable, child.Readable...)),
		Writable:    dedup(append(parent.Writable, child.Writable...)),
		Deny:        dedup(append(parent.Deny, child.Deny...)),
		EnvAllow:    dedup(append(parent.EnvAllow, child.EnvAllow...)),
		EnableGuard: dedup(append(parent.EnableGuard, child.EnableGuard...)),
	}
}

func mergeAdditive(a, b *ResolvedCapability) *ResolvedCapability {
	return &ResolvedCapability{
		Name:        a.Name,
		Sources:     append(a.Sources, b.Sources...),
		Unguard:     dedup(append(a.Unguard, b.Unguard...)),
		Readable:    dedup(append(a.Readable, b.Readable...)),
		Writable:    dedup(append(a.Writable, b.Writable...)),
		Deny:        dedup(append(a.Deny, b.Deny...)),
		EnvAllow:    dedup(append(a.EnvAllow, b.EnvAllow...)),
		EnableGuard: dedup(append(a.EnableGuard, b.EnableGuard...)),
	}
}

func copyStrings(s []string) []string {
	if s == nil {
		return nil
	}
	out := make([]string, len(s))
	copy(out, s)
	return out
}

func copyVisited(m map[string]bool) map[string]bool {
	out := make(map[string]bool, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func dedup(s []string) []string {
	if len(s) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(s))
	var out []string
	for _, v := range s {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

// ResolveAll resolves multiple capability names and returns a merged Set.
func ResolveAll(names []string, registry map[string]Capability, neverAllow, neverAllowEnv []string) (*Set, error) {
	set := &Set{
		NeverAllow:    neverAllow,
		NeverAllowEnv: neverAllowEnv,
	}

	seen := make(map[string]bool)
	for _, name := range names {
		if seen[name] {
			continue
		}
		seen[name] = true

		resolved, err := ResolveOne(name, registry)
		if err != nil {
			return nil, err
		}
		set.Capabilities = append(set.Capabilities, *resolved)
	}

	return set, nil
}

// ToSandboxOverrides merges all capabilities into sandbox policy fields.
func (cs *Set) ToSandboxOverrides() SandboxOverrides {
	var o SandboxOverrides

	for _, rc := range cs.Capabilities {
		o.Unguard = append(o.Unguard, rc.Unguard...)
		o.ReadableExtra = append(o.ReadableExtra, rc.Readable...)
		o.WritableExtra = append(o.WritableExtra, rc.Writable...)
		o.DeniedExtra = append(o.DeniedExtra, rc.Deny...)
		o.EnvAllow = append(o.EnvAllow, rc.EnvAllow...)
		o.EnableGuard = append(o.EnableGuard, rc.EnableGuard...)
	}

	// Append never_allow to denied
	o.DeniedExtra = append(o.DeniedExtra, cs.NeverAllow...)

	// Strip never_allow_env from env_allow
	if len(cs.NeverAllowEnv) > 0 {
		blocked := make(map[string]bool, len(cs.NeverAllowEnv))
		for _, e := range cs.NeverAllowEnv {
			blocked[e] = true
		}
		var filtered []string
		for _, e := range o.EnvAllow {
			if !blocked[e] {
				filtered = append(filtered, e)
			}
		}
		o.EnvAllow = filtered
	}

	o.Unguard = dedup(o.Unguard)
	o.ReadableExtra = dedup(o.ReadableExtra)
	o.WritableExtra = dedup(o.WritableExtra)
	o.DeniedExtra = dedup(o.DeniedExtra)
	o.EnvAllow = dedup(o.EnvAllow)
	o.EnableGuard = dedup(o.EnableGuard)

	return o
}
