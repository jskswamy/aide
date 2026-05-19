package capability

import (
	"fmt"
	"maps"
	"slices"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/fsutil"
	"github.com/jskswamy/aide/internal/homepath"
	"github.com/jskswamy/aide/internal/sliceutil"
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
	Allow       []string
	NetworkMode string
	// Ports is a capability-specific port list. Currently consumed only by
	// the ssh guard.
	Ports []int
	// Markers are top-level detection rules evaluated with OR semantics:
	// any match means this capability applies to the project. Distinct
	// from Variant.Markers (which uses AND).
	Markers []Marker
	// Variants, if non-empty, refines this capability into per-toolchain
	// implementations. The detector walks the list and a consent prompt
	// picks which variants to activate. Variants are an implementation
	// detail — they do not appear in `aide cap list` except as a hint.
	Variants []Variant
	// DefaultVariants names the Variant(s) activated when no markers
	// match or when the user declines the consent prompt. It is the
	// safe fallback for a capability; members must exist in Variants.
	DefaultVariants []string
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
	Allow       []string
	NetworkMode string
	Ports       []int
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
			resolved, err := resolveOne(combineName, registry, maps.Clone(visited), depth+1)
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
		Unguard:     slices.Clone(capDef.Unguard),
		Readable:    slices.Clone(capDef.Readable),
		Writable:    slices.Clone(capDef.Writable),
		Deny:        slices.Clone(capDef.Deny),
		EnvAllow:    slices.Clone(capDef.EnvAllow),
		EnableGuard: slices.Clone(capDef.EnableGuard),
		Allow:       slices.Clone(capDef.Allow),
		NetworkMode: capDef.NetworkMode,
		Ports:       slices.Clone(capDef.Ports),
	}
}

func mergeChild(parent *ResolvedCapability, child *Capability) *ResolvedCapability {
	networkMode := parent.NetworkMode
	if child.NetworkMode != "" {
		networkMode = child.NetworkMode
	}
	return &ResolvedCapability{
		Name:        child.Name,
		Sources:     append([]string{child.Name}, parent.Sources...),
		Unguard:     sliceutil.Dedup(append(parent.Unguard, child.Unguard...)),
		Readable:    sliceutil.Dedup(append(parent.Readable, child.Readable...)),
		Writable:    sliceutil.Dedup(append(parent.Writable, child.Writable...)),
		Deny:        sliceutil.Dedup(append(parent.Deny, child.Deny...)),
		EnvAllow:    sliceutil.Dedup(append(parent.EnvAllow, child.EnvAllow...)),
		EnableGuard: sliceutil.Dedup(append(parent.EnableGuard, child.EnableGuard...)),
		Allow:       sliceutil.Dedup(append(parent.Allow, child.Allow...)),
		NetworkMode: networkMode,
		Ports:       sliceutil.Dedup(append(parent.Ports, child.Ports...)),
	}
}

// MergeSelectedVariants layers the paths/env/guard contributions from
// selected Variants onto a ResolvedCapability, returning a new
// ResolvedCapability that contains the union (deduplicated). The
// input ResolvedCapability is not mutated.
// Fields that variants cannot contribute to (Unguard, Deny, Allow,
// NetworkMode) are copied through without modification so the returned
// ResolvedCapability is fully decoupled from rc.
func MergeSelectedVariants(rc *ResolvedCapability, selected []Variant) *ResolvedCapability {
	out := *rc
	out.Sources = append([]string(nil), rc.Sources...)
	out.Readable = slices.Clone(rc.Readable)
	out.Writable = slices.Clone(rc.Writable)
	out.EnvAllow = slices.Clone(rc.EnvAllow)
	out.EnableGuard = slices.Clone(rc.EnableGuard)
	out.Unguard = slices.Clone(rc.Unguard)
	out.Deny = slices.Clone(rc.Deny)
	out.Allow = slices.Clone(rc.Allow)
	for _, v := range selected {
		if v.Name != "" {
			out.Sources = append(out.Sources, rc.Name+"/"+v.Name)
		}
		out.Readable = sliceutil.Dedup(append(out.Readable, v.Readable...))
		out.Writable = sliceutil.Dedup(append(out.Writable, v.Writable...))
		out.EnvAllow = sliceutil.Dedup(append(out.EnvAllow, v.EnvAllow...))
		out.EnableGuard = sliceutil.Dedup(append(out.EnableGuard, v.EnableGuard...))
	}
	return &out
}

func mergeAdditive(a, b *ResolvedCapability) *ResolvedCapability {
	networkMode := a.NetworkMode
	if b.NetworkMode != "" {
		networkMode = b.NetworkMode
	}
	return &ResolvedCapability{
		Name:        a.Name,
		Sources:     append(a.Sources, b.Sources...),
		Unguard:     sliceutil.Dedup(append(a.Unguard, b.Unguard...)),
		Readable:    sliceutil.Dedup(append(a.Readable, b.Readable...)),
		Writable:    sliceutil.Dedup(append(a.Writable, b.Writable...)),
		Deny:        sliceutil.Dedup(append(a.Deny, b.Deny...)),
		EnvAllow:    sliceutil.Dedup(append(a.EnvAllow, b.EnvAllow...)),
		EnableGuard: sliceutil.Dedup(append(a.EnableGuard, b.EnableGuard...)),
		Allow:       sliceutil.Dedup(append(a.Allow, b.Allow...)),
		NetworkMode: networkMode,
		Ports:       sliceutil.Dedup(append(a.Ports, b.Ports...)),
	}
}

// ResolveAll resolves multiple capability names and returns a merged Set.
//
// After resolution, every path declared on the resolved capabilities (and
// in never_allow) is checked for symlink cycles. A cycle is a config-level
// error — silently falling back would leave the agent with a half-broken
// capability and no diagnostic, so this validation surfaces the offending
// capability + path loudly. Missing paths are NOT errors here: they're
// the common "cache dir created on first run" case and are intentionally
// tolerated (the literal rule still emits; the resolved-target widening
// just no-ops until the path exists).
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
		if err := validateNoSymlinkCycles(resolved); err != nil {
			return nil, err
		}
		set.Capabilities = append(set.Capabilities, *resolved)
	}

	if err := validateNeverAllowNoCycles(neverAllow); err != nil {
		return nil, err
	}

	return set, nil
}

// validateNoSymlinkCycles returns a wrapped error naming the capability and
// path if any Readable/Writable/Deny entry resolves through a symlink cycle.
func validateNoSymlinkCycles(rc *ResolvedCapability) error {
	fields := []struct {
		name  string
		paths []string
	}{
		{"readable", rc.Readable},
		{"writable", rc.Writable},
		{"deny", rc.Deny},
	}
	for _, f := range fields {
		if err := checkCyclesIn(f.paths, func(p string, cause error) error {
			return fmt.Errorf("capability %q: %s path %q: %w", rc.Name, f.name, p, cause)
		}); err != nil {
			return err
		}
	}
	return nil
}

func validateNeverAllowNoCycles(paths []string) error {
	return checkCyclesIn(paths, func(p string, cause error) error {
		return fmt.Errorf("never_allow path %q: %w", p, cause)
	})
}

// checkCyclesIn runs the symlink-cycle check on every entry in paths and
// returns the first cycle error found, wrapped via wrapErr. Returns nil
// when no cycle is found. The wrapErr indirection lets callers attach
// their own diagnostic context (capability name + field, never_allow
// origin, etc.) without each one re-implementing the iteration.
func checkCyclesIn(paths []string, wrapErr func(p string, cause error) error) error {
	for _, p := range paths {
		if err := capabilityCheckCycle(p); err != nil {
			return wrapErr(p, err)
		}
	}
	return nil
}

// capabilityCheckCycle is the capability-layer adapter around
// fsutil.CheckSymlinkCycle. It exists only to apply the
// "tilde-expand against $HOME" convention that user-supplied capability
// paths use; the filesystem-layer primitive deliberately stays pure
// and takes already-resolved paths.
func capabilityCheckCycle(path string) error {
	return fsutil.CheckSymlinkCycle(homepath.Expand(path, ""))
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
		o.Allow = append(o.Allow, rc.Allow...)
		o.SSHPorts = append(o.SSHPorts, rc.Ports...)
		if o.NetworkMode == "" && rc.NetworkMode != "" {
			o.NetworkMode = rc.NetworkMode
		}
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

	o.Unguard = sliceutil.Dedup(o.Unguard)
	o.ReadableExtra = sliceutil.Dedup(o.ReadableExtra)
	o.WritableExtra = sliceutil.Dedup(o.WritableExtra)
	o.DeniedExtra = sliceutil.Dedup(o.DeniedExtra)
	o.EnvAllow = sliceutil.Dedup(o.EnvAllow)
	o.EnableGuard = sliceutil.Dedup(o.EnableGuard)
	o.Allow = sliceutil.Dedup(o.Allow)

	return o
}
