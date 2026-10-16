package config

import (
	"path/filepath"
	"reflect"
	"testing"
)

func boolPtr(b bool) *bool { return &b }

func TestMergeProjectOverrides_LocalScalarsWin(t *testing.T) {
	inherited := &ProjectOverride{Agent: "claude", Secret: "main", Profile: "work", Yolo: boolPtr(true)}
	local := &ProjectOverride{Agent: "codex", Yolo: boolPtr(false)}

	got := MergeProjectOverrides(inherited, local)
	if got.Agent != "codex" {
		t.Errorf("Agent = %q, want codex (local wins)", got.Agent)
	}
	if got.Secret != "main" || got.Profile != "work" {
		t.Errorf("unset local scalars should keep inherited: secret=%q profile=%q", got.Secret, got.Profile)
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
