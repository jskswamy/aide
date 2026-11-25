package launcher

import (
	"slices"
	"testing"

	"github.com/jskswamy/aide/internal/capability"
	"github.com/jskswamy/aide/internal/config"
	aidectx "github.com/jskswamy/aide/internal/context"
)

func TestResolveSandbox_CapabilitiesOverridesAndNoNetworkFlag(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cfg := &config.Config{}
	rc := &aidectx.ResolvedContext{Name: "work", Context: config.Context{
		Agent:        "claude",
		Capabilities: []string{"go"},
		Sandbox: &config.SandboxRef{Inline: &config.SandboxPolicy{
			WritableExtra: []string{"~/out"},
			Network:       &config.NetworkPolicy{Mode: "outbound", AllowPorts: []int{443}, DenyPorts: []int{25}},
		}},
	}}
	in := SandboxInputs{ProjectRoot: t.TempDir(), HomeDir: home, WithoutCaps: []string{"node"}}

	got, err := ResolveSandbox(cfg, rc, in)
	if err != nil {
		t.Fatal(err)
	}
	if !got.ContextCaps["go"] || got.Caps == nil || !slices.ContainsFunc(got.Caps.Capabilities, func(c capability.ResolvedCapability) bool { return c.Name == "go" }) {
		t.Errorf("go capability not resolved from the profile: %+v", got)
	}
	if !slices.Equal(got.ConfigWritable, []string{"~/out"}) {
		t.Errorf("ConfigWritable = %v, want the profile's own list before capability overrides", got.ConfigWritable)
	}
	if got.Config == nil || got.Config.Network.Mode != "outbound" || len(got.Config.Network.AllowPorts) != 1 {
		t.Errorf("network = %+v, want the profile's outbound rule", got.Config)
	}

	in.UnrestrictedNetwork = true
	got, err = ResolveSandbox(cfg, rc, in)
	if err != nil {
		t.Fatal(err)
	}
	n := got.Config.Network
	if n.Mode != "unrestricted" || len(n.AllowPorts) != 0 || len(n.DenyPorts) != 0 {
		t.Errorf("-N: network = %+v, want unrestricted with no port rules", n)
	}
}

func TestResolveSandbox_UnknownCapabilityErrors(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	rc := &aidectx.ResolvedContext{Name: "work", Context: config.Context{Agent: "claude", Capabilities: []string{"goo"}}}
	_, err := ResolveSandbox(&config.Config{}, rc, SandboxInputs{ProjectRoot: t.TempDir(), HomeDir: t.TempDir()})
	if err == nil {
		t.Fatal("want an error for an unknown capability")
	}
}
