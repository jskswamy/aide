package config

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"
)

func boolPtr(b bool) *bool { return &b }

func TestMergeProjectOverrides_LocalScalarsWin(t *testing.T) {
	inherited := &ProjectOverride{Agent: "claude", Profile: "work", Yolo: boolPtr(true)}
	local := &ProjectOverride{Agent: "codex", Yolo: boolPtr(false)}

	got := MergeProjectOverrides(inherited, local)
	if got.Agent != "codex" {
		t.Errorf("Agent = %q, want codex (local wins)", got.Agent)
	}
	if got.Profile != "work" {
		t.Errorf("unset local scalars should keep inherited: profile=%q", got.Profile)
	}
	if got.Yolo == nil || *got.Yolo {
		t.Errorf("Yolo = %v, want false (local wins)", got.Yolo)
	}
}

func TestMergeProjectOverrides_CapabilitiesStableUnion(t *testing.T) {
	inherited := &ProjectOverride{Capabilities: []string{"go", "docker"}}
	local := &ProjectOverride{Capabilities: []string{"node", "go"}}
	got := MergeProjectOverrides(inherited, local)
	if want := []string{"go", "docker", "node"}; !reflect.DeepEqual(got.Capabilities, want) {
		t.Errorf("Capabilities = %v, want %v", got.Capabilities, want)
	}
}

// Ratified: a worktree's disabled_capabilities may subtract inherited
// capabilities; a local capability re-enables one the main checkout
// disabled.
func TestMergeProjectOverrides_LocalDisabledSubtractsInherited(t *testing.T) {
	inherited := &ProjectOverride{Capabilities: []string{"go", "docker"}, DisabledCapabilities: []string{"aws"}}
	local := &ProjectOverride{Capabilities: []string{"aws"}, DisabledCapabilities: []string{"docker"}}
	got := MergeProjectOverrides(inherited, local)
	if want := []string{"go", "aws"}; !reflect.DeepEqual(got.Capabilities, want) {
		t.Errorf("Capabilities = %v, want %v", got.Capabilities, want)
	}
	if want := []string{"docker"}; !reflect.DeepEqual(got.DisabledCapabilities, want) {
		t.Errorf("DisabledCapabilities = %v, want %v", got.DisabledCapabilities, want)
	}
}

func TestMergeProjectOverrides_EnvKeyWise(t *testing.T) {
	inherited := &ProjectOverride{Env: map[string]string{"A": "main", "B": "main"}}
	local := &ProjectOverride{Env: map[string]string{"B": "local", "C": "local"}}
	got := MergeProjectOverrides(inherited, local)
	want := map[string]string{"A": "main", "B": "local", "C": "local"}
	if !reflect.DeepEqual(got.Env, want) {
		t.Errorf("Env = %v, want %v", got.Env, want)
	}
	if inherited.Env["B"] != "main" {
		t.Error("merge must not modify the inherited layer")
	}
}

func TestMergeProjectOverrides_SandboxKeepsOverrideSemantics(t *testing.T) {
	inherited := &ProjectOverride{Sandbox: &SandboxPolicy{
		WritableExtra: []string{"/main/a"},
		Denied:        []string{"/main/denied"},
		Network:       &NetworkPolicy{Mode: "unrestricted"},
	}}
	local := &ProjectOverride{Sandbox: &SandboxPolicy{
		WritableExtra: []string{"/wt/b", "/main/a"},
		Network:       &NetworkPolicy{Mode: "none"},
	}}
	got := MergeProjectOverrides(inherited, local)
	if want := []string{"/main/a", "/wt/b"}; !reflect.DeepEqual(got.Sandbox.WritableExtra, want) {
		t.Errorf("WritableExtra = %v, want %v (additive)", got.Sandbox.WritableExtra, want)
	}
	if want := []string{"/main/denied"}; !reflect.DeepEqual(got.Sandbox.Denied, want) {
		t.Errorf("Denied = %v, want inherited %v", got.Sandbox.Denied, want)
	}
	if got.Sandbox.Network == nil || got.Sandbox.Network.Mode != "none" {
		t.Errorf("Network = %+v, want local none", got.Sandbox.Network)
	}
}

func TestMergeProjectOverrides_NilLayers(t *testing.T) {
	if MergeProjectOverrides(nil, nil) != nil {
		t.Error("nil + nil should be nil")
	}
	local := &ProjectOverride{Agent: "codex"}
	if got := MergeProjectOverrides(nil, local); got == nil || got.Agent != "codex" {
		t.Errorf("nil + local = %+v", got)
	}
}

// Relative paths are interpreted against the checkout holding the
// declaring file, never the shell's current directory.
func TestRewriteRelativePaths(t *testing.T) {
	root := filepath.FromSlash("/repos/main")
	po := &ProjectOverride{Sandbox: &SandboxPolicy{
		Writable:      []string{"build", "/abs/out"},
		WritableExtra: []string{"./cache", "~/.npm", "{{ .project_root }}/x", "$HOME/y"},
		ReadableExtra: []string{"../shared"},
		DeniedExtra:   []string{"secrets/*.pem"},
	}}
	rewriteRelativePaths(po, root)
	sb := po.Sandbox
	check := func(field string, got, want []string) {
		t.Helper()
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s = %v, want %v", field, got, want)
		}
	}
	check("Writable", sb.Writable, []string{filepath.Join(root, "build"), "/abs/out"})
	check("WritableExtra", sb.WritableExtra, []string{filepath.Join(root, "cache"), "~/.npm", "{{ .project_root }}/x", "$HOME/y"})
	check("ReadableExtra", sb.ReadableExtra, []string{filepath.Join(root, "../shared")})
	check("DeniedExtra", sb.DeniedExtra, []string{filepath.Join(root, "secrets/*.pem")})
}

func TestDropProjectConfig_ClearsEverything(t *testing.T) {
	c := &Config{
		ProjectOverride:       &ProjectOverride{Agent: "claude"},
		ProjectLayers:         []ProjectLayer{{Path: "/a/.aide.yaml"}},
		ProjectConfigErr:      errors.New("boom"),
		ProjectConfigWarnings: []string{"warn"},
	}
	c.DropProjectConfig()
	if c.ProjectOverride != nil || c.ProjectLayers != nil || c.ProjectConfigErr != nil || c.ProjectConfigWarnings != nil {
		t.Errorf("DropProjectConfig left state behind: %+v", c)
	}
}

func TestProjectFileSetting_LastAcceptedLayerWithTheSettingWins(t *testing.T) {
	hasAgent := func(po *ProjectOverride) bool { return po.Agent != "" }
	c := &Config{
		ProjectConfigPath: "/fallback/.aide.yaml",
		ProjectLayers: []ProjectLayer{
			{Path: "/main/.aide.yaml", Override: &ProjectOverride{Agent: "claude"}},
			{Path: "/wt/.aide.yaml", Override: &ProjectOverride{}},
			{Path: "/rejected/.aide.yaml", Override: &ProjectOverride{Agent: "codex"}, Rejected: true},
		},
	}
	if got := c.ProjectFileSetting(hasAgent); got != "/main/.aide.yaml" {
		t.Errorf("ProjectFileSetting = %q, want the last accepted layer that sets it", got)
	}
	if got := c.ProjectFileSetting(func(*ProjectOverride) bool { return false }); got != "/fallback/.aide.yaml" {
		t.Errorf("ProjectFileSetting with no match = %q, want ProjectConfigPath", got)
	}
}

func TestMergeProjectOverrides_NilSides(t *testing.T) {
	if got := MergeProjectOverrides(nil, nil); got != nil {
		t.Errorf("both nil = %+v, want nil", got)
	}
	po := &ProjectOverride{Agent: "claude", MCPServers: []string{"a"}}
	if got := MergeProjectOverrides(nil, po); got.Agent != "claude" {
		t.Errorf("nil base: Agent = %q", got.Agent)
	}
	if got := MergeProjectOverrides(po, nil); got.Agent != "claude" {
		t.Errorf("nil top: Agent = %q", got.Agent)
	}
}

func TestMergeProjectOverrides_MCPServersAndVariants(t *testing.T) {
	base := &ProjectOverride{MCPServers: []string{"a"}, CapabilityVariants: map[string][]string{"go": {"1.22"}, "node": {"20"}}}
	top := &ProjectOverride{MCPServers: []string{"b"}, CapabilityVariants: map[string][]string{"go": {"1.23"}}}
	got := MergeProjectOverrides(base, top)
	if !reflect.DeepEqual(got.MCPServers, []string{"b"}) {
		t.Errorf("MCPServers = %v, want the local list to replace", got.MCPServers)
	}
	want := map[string][]string{"go": {"1.23"}, "node": {"20"}}
	if !reflect.DeepEqual(got.CapabilityVariants, want) {
		t.Errorf("CapabilityVariants = %v, want %v", got.CapabilityVariants, want)
	}
}

func TestMergeProjectOverrides_SandboxFields(t *testing.T) {
	no, yes := boolPtr(false), boolPtr(true)
	base := &ProjectOverride{Sandbox: &SandboxPolicy{
		Writable: []string{"/w"}, WritableExtra: []string{"/we"}, Network: &NetworkPolicy{}, AllowSubprocess: yes,
	}}
	top := &ProjectOverride{Sandbox: &SandboxPolicy{
		Writable: []string{"/w2"}, Readable: []string{"/r"}, Denied: []string{"/d"}, Guards: []string{"g"},
		WritableExtra: []string{"/we2"}, ReadableExtra: []string{"/re"}, DeniedExtra: []string{"/de"},
		GuardsExtra: []string{"ge"}, Unguard: []string{"ug"}, Allow: []string{"al"},
		AllowSubprocess: no, CleanEnv: yes,
	}}
	got := MergeProjectOverrides(base, top).Sandbox
	if !reflect.DeepEqual(got.Writable, []string{"/w2"}) || !reflect.DeepEqual(got.WritableExtra, []string{"/we", "/we2"}) {
		t.Errorf("writable: replace/extend wrong: %v %v", got.Writable, got.WritableExtra)
	}
	if !reflect.DeepEqual(got.Readable, []string{"/r"}) || !reflect.DeepEqual(got.Denied, []string{"/d"}) || !reflect.DeepEqual(got.Guards, []string{"g"}) {
		t.Errorf("replaced lists wrong: %v %v %v", got.Readable, got.Denied, got.Guards)
	}
	if !reflect.DeepEqual(got.ReadableExtra, []string{"/re"}) || !reflect.DeepEqual(got.DeniedExtra, []string{"/de"}) ||
		!reflect.DeepEqual(got.GuardsExtra, []string{"ge"}) || !reflect.DeepEqual(got.Unguard, []string{"ug"}) || !reflect.DeepEqual(got.Allow, []string{"al"}) {
		t.Errorf("extra lists wrong: %+v", got)
	}
	if got.Network == nil {
		t.Error("inherited Network was dropped")
	}
	if got.AllowSubprocess == nil || *got.AllowSubprocess || got.CleanEnv == nil || !*got.CleanEnv {
		t.Errorf("scalar pointers: AllowSubprocess=%v CleanEnv=%v", got.AllowSubprocess, got.CleanEnv)
	}
}

func TestMergeProjectOverrides_SandboxOneSided(t *testing.T) {
	p := &SandboxPolicy{WritableExtra: []string{"/x"}}
	if got := MergeProjectOverrides(&ProjectOverride{Sandbox: p}, &ProjectOverride{}).Sandbox; !reflect.DeepEqual(got.WritableExtra, []string{"/x"}) || got == p {
		t.Errorf("inherited-only sandbox: want a copy of the policy, got %+v (same pointer: %v)", got, got == p)
	}
	off := &SandboxPolicy{Disabled: true}
	if got := MergeProjectOverrides(&ProjectOverride{Sandbox: p}, &ProjectOverride{Sandbox: off}).Sandbox; !got.Disabled {
		t.Error("sandbox: false in the worktree must stand")
	}
}

func TestMergeProjectOverrides_PreferencesAndStatusline(t *testing.T) {
	yes, no := boolPtr(true), boolPtr(false)
	base := &ProjectOverride{
		Preferences: &Preferences{ShowInfo: yes, InfoStyle: "compact", Yolo: yes},
		Statusline:  &StatuslineConfig{Order: []string{"sandbox"}},
	}
	top := &ProjectOverride{
		Preferences: &Preferences{ShowInfo: no, InfoDetail: "full"},
		Statusline:  &StatuslineConfig{Order: []string{"caps", "sandbox"}},
	}
	got := MergeProjectOverrides(base, top)
	p := got.Preferences
	if p.ShowInfo == nil || *p.ShowInfo || p.InfoStyle != "compact" || p.InfoDetail != "full" || p.Yolo == nil || !*p.Yolo {
		t.Errorf("Preferences = %+v", p)
	}
	if !reflect.DeepEqual(got.Statusline.Order, []string{"caps", "sandbox"}) {
		t.Errorf("Statusline.Order = %v, want the local order", got.Statusline.Order)
	}
	// A side with neither set stays unset.
	if m := MergeProjectOverrides(&ProjectOverride{}, &ProjectOverride{}); m.Preferences != nil || m.Statusline != nil {
		t.Errorf("empty merge invented preferences/statusline: %+v", m)
	}
}
