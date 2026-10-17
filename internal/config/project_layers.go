package config

import (
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"github.com/jskswamy/aide/internal/sliceutil"
)

// Project layer sources.
const (
	// LayerInherited is a linked worktree's main checkout .aide.yaml.
	LayerInherited = "inherited"
	// LayerLocal is the .aide.yaml of the checkout aide runs in.
	LayerLocal = "local"
)

// ProjectLayer is one .aide.yaml that applies to a launch. A linked
// worktree has up to two: the main checkout's file (inherited), then
// its own (local). Each layer is trust-checked on its own.
type ProjectLayer struct {
	// Source is LayerInherited or LayerLocal.
	Source string
	// Path is the file as discovered.
	Path string
	// Contents are the exact bytes Override was parsed from; trust is
	// judged on these bytes so what is approved is what applies.
	Contents []byte
	// Override has relative paths already rewritten against the
	// checkout containing the file.
	Override *ProjectOverride
	// Rejected is set by the trust gate when the file is untrusted or
	// denied; a rejected layer contributes nothing.
	Rejected bool
}

// MergeProjectLayers merges the layers that are not rejected, in order
// (inherited, then local). It returns nil when none apply and a single
// applied layer's override as is.
func MergeProjectLayers(layers []ProjectLayer) *ProjectOverride {
	var applied []*ProjectOverride
	for _, l := range layers {
		if !l.Rejected {
			applied = append(applied, l.Override)
		}
	}
	if len(applied) == 1 {
		return applied[0]
	}
	var merged *ProjectOverride
	for _, po := range applied {
		merged = MergeProjectOverrides(merged, po)
	}
	return merged
}

// DropProjectConfig removes all .aide.yaml settings from c, as
// --ignore-project-config and a failed trust gate do.
func (c *Config) DropProjectConfig() {
	c.ProjectOverride = nil
	c.ProjectLayers = nil
	c.ProjectConfigErr = nil
	c.ProjectConfigWarnings = nil
}

// ProjectFileSetting names the .aide.yaml whose setting applies: the
// last layer for which has is true (local wins over inherited), else
// ProjectConfigPath. Messages use it to point at the right file.
func (c *Config) ProjectFileSetting(has func(*ProjectOverride) bool) string {
	for i := len(c.ProjectLayers) - 1; i >= 0; i-- {
		if po := c.ProjectLayers[i].Override; po != nil && !c.ProjectLayers[i].Rejected && has(po) {
			return c.ProjectLayers[i].Path
		}
	}
	return c.ProjectConfigPath
}

// MergeProjectOverrides layers top (worktree-local) over base
// (inherited) without modifying either. Scalars in top win;
// capabilities are a stable union and top's disabled_capabilities may
// subtract inherited ones; env and capability_variants merge key-wise
// with top winning; sandbox fields keep the semantics a project
// override has over a profile (extras add, other fields replace).
func MergeProjectOverrides(base, top *ProjectOverride) *ProjectOverride {
	if base == nil && top == nil {
		return nil
	}
	if base == nil {
		base = &ProjectOverride{}
	}
	if top == nil {
		top = &ProjectOverride{}
	}
	out := &ProjectOverride{
		Profile:     firstNonEmpty(top.Profile, base.Profile),
		Agent:       firstNonEmpty(top.Agent, base.Agent),
		Yolo:        firstNonNil(top.Yolo, base.Yolo),
		MCPServers:  slices.Clone(base.MCPServers),
		Env:         mergeMaps(base.Env, top.Env),
		Sandbox:     mergeSandboxPolicies(base.Sandbox, top.Sandbox),
		Preferences: mergePreferences(base.Preferences, top.Preferences),
		Statusline:  mergeStatusline(base.Statusline, top.Statusline),
	}
	if len(top.MCPServers) > 0 {
		out.MCPServers = slices.Clone(top.MCPServers)
	}
	caps := sliceutil.Dedup(append(slices.Clone(base.Capabilities), top.Capabilities...))
	out.Capabilities = without(caps, top.DisabledCapabilities)
	disabled := sliceutil.Dedup(append(without(base.DisabledCapabilities, top.Capabilities), top.DisabledCapabilities...))
	out.DisabledCapabilities = disabled
	if len(base.CapabilityVariants)+len(top.CapabilityVariants) > 0 {
		out.CapabilityVariants = make(map[string][]string)
		for k, v := range base.CapabilityVariants {
			out.CapabilityVariants[k] = slices.Clone(v)
		}
		for k, v := range top.CapabilityVariants {
			out.CapabilityVariants[k] = slices.Clone(v)
		}
	}
	return out
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func firstNonNil[T any](a, b *T) *T {
	if a != nil {
		return a
	}
	return b
}

func mergeMaps(base, top map[string]string) map[string]string {
	if len(base)+len(top) == 0 {
		return nil
	}
	out := maps.Clone(base)
	if out == nil {
		out = make(map[string]string, len(top))
	}
	maps.Copy(out, top)
	return out
}

// without returns a's elements not in b, in a's order; nil when empty.
func without(a, b []string) []string {
	var out []string
	for _, v := range a {
		if !slices.Contains(b, v) {
			out = append(out, v)
		}
	}
	return out
}

func mergeSandboxPolicies(base, top *SandboxPolicy) *SandboxPolicy {
	switch {
	case base == nil && top == nil:
		return nil
	case top == nil:
		return cloneSandboxPolicy(base)
	case base == nil, top.Disabled, base.Disabled:
		// sandbox: false on either side is a whole-value setting:
		// the worktree's choice stands.
		return cloneSandboxPolicy(top)
	}
	out := cloneSandboxPolicy(base)
	out.WritableExtra = sliceutil.Dedup(append(out.WritableExtra, top.WritableExtra...))
	out.ReadableExtra = sliceutil.Dedup(append(out.ReadableExtra, top.ReadableExtra...))
	out.DeniedExtra = sliceutil.Dedup(append(out.DeniedExtra, top.DeniedExtra...))
	out.GuardsExtra = sliceutil.Dedup(append(out.GuardsExtra, top.GuardsExtra...))
	out.Unguard = sliceutil.Dedup(append(out.Unguard, top.Unguard...))
	out.Allow = sliceutil.Dedup(append(out.Allow, top.Allow...))
	if len(top.Writable) > 0 {
		out.Writable = slices.Clone(top.Writable)
	}
	if len(top.Readable) > 0 {
		out.Readable = slices.Clone(top.Readable)
	}
	if len(top.Denied) > 0 {
		out.Denied = slices.Clone(top.Denied)
	}
	if len(top.Guards) > 0 {
		out.Guards = slices.Clone(top.Guards)
	}
	if top.Network != nil {
		out.Network = top.Network
	}
	if top.AllowSubprocess != nil {
		out.AllowSubprocess = top.AllowSubprocess
	}
	if top.CleanEnv != nil {
		out.CleanEnv = top.CleanEnv
	}
	return out
}

func cloneSandboxPolicy(p *SandboxPolicy) *SandboxPolicy {
	c := *p
	c.Writable = slices.Clone(p.Writable)
	c.Readable = slices.Clone(p.Readable)
	c.Denied = slices.Clone(p.Denied)
	c.WritableExtra = slices.Clone(p.WritableExtra)
	c.ReadableExtra = slices.Clone(p.ReadableExtra)
	c.DeniedExtra = slices.Clone(p.DeniedExtra)
	c.Guards = slices.Clone(p.Guards)
	c.GuardsExtra = slices.Clone(p.GuardsExtra)
	c.Unguard = slices.Clone(p.Unguard)
	c.Allow = slices.Clone(p.Allow)
	return &c
}

func mergePreferences(base, top *Preferences) *Preferences {
	if base == nil && top == nil {
		return nil
	}
	out := &Preferences{}
	for _, p := range []*Preferences{base, top} {
		if p == nil {
			continue
		}
		out.ShowInfo = firstNonNil(p.ShowInfo, out.ShowInfo)
		out.Yolo = firstNonNil(p.Yolo, out.Yolo)
		out.InfoStyle = firstNonEmpty(p.InfoStyle, out.InfoStyle)
		out.InfoDetail = firstNonEmpty(p.InfoDetail, out.InfoDetail)
	}
	return out
}

func mergeStatusline(base, top *StatuslineConfig) *StatuslineConfig {
	if base == nil && top == nil {
		return nil
	}
	out := &StatuslineConfig{}
	for _, s := range []*StatuslineConfig{base, top} {
		if s != nil {
			applyStatuslineOverride(out, s)
		}
	}
	return out
}

// hasRelativePaths reports whether po has sandbox paths that
// rewriteRelativePaths would anchor.
func hasRelativePaths(po *ProjectOverride) bool {
	if po == nil || po.Sandbox == nil {
		return false
	}
	sb := po.Sandbox
	for _, list := range [][]string{sb.Writable, sb.Readable, sb.Denied, sb.WritableExtra, sb.ReadableExtra, sb.DeniedExtra} {
		if slices.ContainsFunc(list, isRelativeConfigPath) {
			return true
		}
	}
	return false
}

// rewriteRelativePaths makes po's relative sandbox paths absolute
// against root, the checkout containing the declaring file. Paths that
// are absolute, home-relative (~, $HOME) or templated are left alone.
func rewriteRelativePaths(po *ProjectOverride, root string) {
	if po == nil || po.Sandbox == nil || root == "" {
		return
	}
	sb := po.Sandbox
	for _, list := range []*[]string{
		&sb.Writable, &sb.Readable, &sb.Denied,
		&sb.WritableExtra, &sb.ReadableExtra, &sb.DeniedExtra,
	} {
		for i, p := range *list {
			if isRelativeConfigPath(p) {
				(*list)[i] = filepath.Join(root, p)
			}
		}
	}
}

func isRelativeConfigPath(p string) bool {
	if p == "" || filepath.IsAbs(p) {
		return false
	}
	for _, prefix := range []string{"~", "$", "{{"} {
		if strings.HasPrefix(p, prefix) {
			return false
		}
	}
	return true
}
