package launcher

import (
	"fmt"
	"os"
	"slices"
	"sort"

	"github.com/jskswamy/aide/internal/config"
	"github.com/jskswamy/aide/internal/homepath"
	"github.com/jskswamy/aide/internal/trust"
	"github.com/jskswamy/aide/internal/ui"
)

// ProjectGate is what the .aide.yaml trust gate decided.
type ProjectGate struct {
	// Files has one entry per untrusted or denied file, in layer
	// order (inherited, then local). Those files contribute nothing.
	Files []ui.TrustInfo
	// InheritedGrants are the security-sensitive grants the trusted
	// main checkout file of a linked worktree contributes.
	InheritedGrants []ui.InheritedGrant
}

// Primary is the first untrusted or denied file, or nil when every
// file is trusted.
func (g ProjectGate) Primary() *ui.TrustInfo {
	if len(g.Files) == 0 {
		return nil
	}
	return &g.Files[0]
}

// Status summarizes the gate as "denied", "untrusted" or "trusted".
func (g ProjectGate) Status() string {
	status := "trusted"
	for _, f := range g.Files {
		if f.Status == "denied" {
			return "denied"
		}
		status = f.Status
	}
	return status
}

// SetBanner records the gate's trust files and inherited grants on
// banner data.
func (g ProjectGate) SetBanner(data *ui.BannerData) {
	data.Trust = g.Primary()
	data.TrustFiles = g.Files
	data.InheritedGrants = g.InheritedGrants
}

// GateProjectConfig applies the launch's .aide.yaml trust gate to cfg
// with the default trust store: each file is judged on its own and an
// untrusted or denied file contributes nothing. Previews (which,
// status, prompt, statusline, aide project) call it so they agree
// with what a launch would do. It returns the diagnostic for a linked
// worktree whose main checkout cannot be read.
func GateProjectConfig(cfg *config.Config) (ProjectGate, error) {
	return (&Launcher{}).applyTrustGate(cfg)
}

// applyTrustGate keeps the trusted .aide.yaml layers of cfg and
// re-merges ProjectOverride from them.
func (l *Launcher) applyTrustGate(cfg *config.Config) (ProjectGate, error) {
	if err := cfg.ProjectConfigErr; err != nil {
		cfg.DropProjectConfig()
		return ProjectGate{}, err
	}
	layers, ok := l.projectLayers(cfg)
	if !ok {
		cfg.DropProjectConfig()
		return ProjectGate{}, nil
	}
	homeDir, _ := os.UserHomeDir()
	store := l.trustStore()
	var gate ProjectGate
	var kept []config.ProjectLayer
	for _, layer := range layers {
		fc := store.CheckContents(layer.Path, layer.Contents)
		display := homepath.Collapse(fc.Path, homeDir)
		switch fc.Status {
		case trust.Trusted:
			kept = append(kept, layer)
		case trust.Denied:
			gate.Files = append(gate.Files, ui.TrustInfo{Status: "denied", Path: display, Source: layer.Source})
		default:
			gate.Files = append(gate.Files, buildTrustInfo(display, layer.Source, layer.Override))
		}
	}
	cfg.ProjectLayers = kept
	cfg.ProjectOverride = config.MergeProjectLayers(kept)
	gate.InheritedGrants = inheritedGrants(kept, cfg.ProjectOverride, homeDir)
	return gate, nil
}

// projectLayers returns cfg's layers. A config built without Load
// (ProjectOverride and ProjectConfigPath only) is one local layer
// judged on the file's current contents; ok is false when it cannot
// be read.
func (l *Launcher) projectLayers(cfg *config.Config) ([]config.ProjectLayer, bool) {
	if len(cfg.ProjectLayers) > 0 {
		return cfg.ProjectLayers, true
	}
	if cfg.ProjectOverride == nil || cfg.ProjectConfigPath == "" {
		return nil, true
	}
	contents, err := os.ReadFile(cfg.ProjectConfigPath)
	if err != nil {
		fmt.Fprintf(l.stderr(), "warning: could not read %s for trust check: %v — project config not applied\n", cfg.ProjectConfigPath, err)
		return nil, false
	}
	return []config.ProjectLayer{{
		Source: config.LayerLocal, Path: cfg.ProjectConfigPath, Contents: contents, Override: cfg.ProjectOverride,
	}}, true
}

// buildTrustInfo describes what an untrusted file requests.
func buildTrustInfo(displayPath, source string, po *config.ProjectOverride) ui.TrustInfo {
	ti := ui.TrustInfo{Status: "untrusted", Path: displayPath, Source: source}
	if po == nil {
		return ti
	}
	ti.Wants = ui.TrustWants{
		Agent:        po.Agent,
		Capabilities: po.Capabilities,
	}
	if po.Sandbox != nil {
		ti.Wants.Writable = append(slices.Clone(po.Sandbox.Writable), po.Sandbox.WritableExtra...)
		ti.Wants.Unguard = po.Sandbox.Unguard
	}
	for k := range po.Env {
		ti.Wants.EnvVars = append(ti.Wants.EnvVars, k)
	}
	sort.Strings(ti.Wants.EnvVars)
	return ti
}

// inheritedGrants lists the security-sensitive grants that the kept
// inherited layer contributes and that survive the merge: paths made
// writable or readable, network mode, removed guards, capabilities,
// a disabled sandbox and auto-approve.
func inheritedGrants(kept []config.ProjectLayer, merged *config.ProjectOverride, homeDir string) []ui.InheritedGrant {
	var inh *config.ProjectLayer
	for i := range kept {
		if kept[i].Source == config.LayerInherited {
			inh = &kept[i]
		}
	}
	if inh == nil || merged == nil {
		return nil
	}
	src := homepath.Collapse(inh.Path, homeDir)
	var out []ui.InheritedGrant
	add := func(kind, value string) {
		out = append(out, ui.InheritedGrant{Kind: kind, Value: value, Source: src})
	}
	po := inh.Override
	for _, c := range po.Capabilities {
		if slices.Contains(merged.Capabilities, c) {
			add("capability", c)
		}
	}
	if sb, msb := po.Sandbox, merged.Sandbox; sb != nil && msb != nil {
		if sb.Disabled && msb.Disabled {
			add("sandbox", "disabled")
		}
		for _, p := range append(slices.Clone(sb.Writable), sb.WritableExtra...) {
			if slices.Contains(msb.Writable, p) || slices.Contains(msb.WritableExtra, p) {
				add("writable", p)
			}
		}
		for _, p := range append(slices.Clone(sb.Readable), sb.ReadableExtra...) {
			if slices.Contains(msb.Readable, p) || slices.Contains(msb.ReadableExtra, p) {
				add("readable", p)
			}
		}
		if sb.Network != nil && msb.Network == sb.Network {
			add("network", sb.Network.Mode)
		}
		for _, g := range sb.Unguard {
			if slices.Contains(msb.Unguard, g) {
				add("unguard", g)
			}
		}
	}
	if po.Yolo != nil && *po.Yolo && merged.Yolo == po.Yolo {
		add("auto_approve", "on")
	}
	return out
}
