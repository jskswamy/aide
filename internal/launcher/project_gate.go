package launcher

import (
	"fmt"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"

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
	// Warnings explain project files that were deliberately not
	// considered (a checkout not registered with the repository it
	// names).
	Warnings []string
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

// applyTrustGate judges every .aide.yaml layer of cfg, marks the
// untrusted and denied ones rejected and re-merges ProjectOverride from
// the rest. It is idempotent: calling it again reports the same files.
func (l *Launcher) applyTrustGate(cfg *config.Config) (ProjectGate, error) {
	if err := cfg.ProjectConfigErr; err != nil {
		cfg.DropProjectConfig()
		return ProjectGate{}, err
	}
	var gate ProjectGate
	if w := cfg.ProjectConfigWarning; w != "" {
		gate.Warnings = append(gate.Warnings, w)
	}
	if !l.ensureProjectLayers(cfg) {
		cfg.DropProjectConfig()
		return gate, nil
	}
	if len(cfg.ProjectLayers) == 0 {
		return gate, nil // no project file: nothing to judge
	}
	homeDir, _ := os.UserHomeDir()
	store := l.trustStore()
	for i := range cfg.ProjectLayers {
		layer := &cfg.ProjectLayers[i]
		fc := store.CheckContents(layer.Path, layer.Contents)
		display := homepath.Collapse(fc.Path, homeDir)
		layer.Rejected = fc.Status != trust.Trusted
		switch fc.Status {
		case trust.Trusted:
		case trust.Denied:
			gate.Files = append(gate.Files, ui.TrustInfo{Status: "denied", Path: display, Source: layer.Source})
		default:
			gate.Files = append(gate.Files, buildTrustInfo(display, layer.Source, layer.Override))
		}
	}
	cfg.ProjectOverride = config.MergeProjectLayers(cfg.ProjectLayers)
	gate.InheritedGrants = inheritedGrants(cfg.ProjectLayers, cfg.ProjectOverride, homeDir)
	return gate, nil
}

// ensureProjectLayers makes sure cfg's project config is held as
// layers. A config built without Load (ProjectOverride and
// ProjectConfigPath only) becomes one local layer judged on the file's
// current contents; it returns false when that file cannot be read.
func (l *Launcher) ensureProjectLayers(cfg *config.Config) bool {
	if len(cfg.ProjectLayers) > 0 || cfg.ProjectOverride == nil || cfg.ProjectConfigPath == "" {
		return true
	}
	contents, err := os.ReadFile(cfg.ProjectConfigPath)
	if err != nil {
		fmt.Fprintf(l.stderr(), "warning: could not read %s for trust check: %v — project config not applied\n", cfg.ProjectConfigPath, err)
		return false
	}
	cfg.ProjectLayers = []config.ProjectLayer{{
		Source: config.LayerLocal, Path: cfg.ProjectConfigPath, Contents: contents, Override: cfg.ProjectOverride,
	}}
	return true
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
	if sb := po.Sandbox; sb != nil {
		ti.Wants.Writable = append(slices.Clone(sb.Writable), sb.WritableExtra...)
		ti.Wants.Readable = append(slices.Clone(sb.Readable), sb.ReadableExtra...)
		ti.Wants.Unguard = sb.Unguard
		ti.Wants.Guards = sb.Guards
		ti.Wants.Allow = sb.Allow
		ti.Wants.SandboxDisabled = sb.Disabled
		if sb.Network != nil {
			ti.Wants.Network = sb.Network.Mode
		}
	}
	ti.Wants.AutoApprove = po.Yolo != nil && *po.Yolo
	for k := range po.Env {
		ti.Wants.EnvVars = append(ti.Wants.EnvVars, k)
	}
	sort.Strings(ti.Wants.EnvVars)
	return ti
}

// inheritedGrants lists the security-sensitive settings that the
// applied inherited layer contributes and that survive the merge:
// capabilities, MCP servers, paths made writable or readable, replaced
// guard or deny lists, removed guards, extra allow rules, network mode,
// subprocess and environment loosening, a disabled sandbox and
// auto-approve.
func inheritedGrants(layers []config.ProjectLayer, merged *config.ProjectOverride, homeDir string) []ui.InheritedGrant {
	var inh *config.ProjectLayer
	for i := range layers {
		if layers[i].Source == config.LayerInherited && !layers[i].Rejected {
			inh = &layers[i]
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
	addEach := func(kind string, values, mergedValues []string) {
		for _, v := range values {
			if slices.Contains(mergedValues, v) {
				add(kind, v)
			}
		}
	}
	po := inh.Override
	addEach("capability", po.Capabilities, merged.Capabilities)
	addEach("mcp_server", po.MCPServers, merged.MCPServers)
	if sb, msb := po.Sandbox, merged.Sandbox; sb != nil && msb != nil {
		if sb.Disabled && msb.Disabled {
			add("sandbox", "disabled")
		}
		addEach("writable", append(slices.Clone(sb.Writable), sb.WritableExtra...), append(slices.Clone(msb.Writable), msb.WritableExtra...))
		addEach("readable", append(slices.Clone(sb.Readable), sb.ReadableExtra...), append(slices.Clone(msb.Readable), msb.ReadableExtra...))
		// Replacing the guard or deny list can drop defaults.
		if len(sb.Guards) > 0 && slices.Equal(sb.Guards, msb.Guards) {
			add("guards", strings.Join(sb.Guards, ", "))
		}
		if len(sb.Denied) > 0 && slices.Equal(sb.Denied, msb.Denied) {
			add("denied", strings.Join(sb.Denied, ", "))
		}
		addEach("unguard", sb.Unguard, msb.Unguard)
		addEach("allow", sb.Allow, msb.Allow)
		if sb.Network != nil && msb.Network == sb.Network {
			add("network", sb.Network.Mode)
		}
		if sb.AllowSubprocess != nil && msb.AllowSubprocess == sb.AllowSubprocess {
			add("allow_subprocess", strconv.FormatBool(*sb.AllowSubprocess))
		}
		if sb.CleanEnv != nil && !*sb.CleanEnv && msb.CleanEnv == sb.CleanEnv {
			add("clean_env", "false")
		}
	}
	if po.Yolo != nil && *po.Yolo && merged.Yolo == po.Yolo {
		add("auto_approve", "on")
	}
	return out
}
