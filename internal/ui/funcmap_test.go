package ui

import (
	"strings"
	"testing"

	"github.com/fatih/color"
)

func init() {
	color.NoColor = true
}

func TestColorFuncMap_HasAllKeys(t *testing.T) {
	fm := colorFuncMap()
	required := []string{
		"bold", "green", "boldGreen", "yellow", "dim", "red", "cyan",
		"agentDisplay", "secretDisplay", "envLines", "networkLabel",
		"truncate", "join", "hasItems", "slice",
		"sandboxDisabled", "sandboxPorts", "hasCapOrExtra",
	}
	for _, key := range required {
		if _, ok := fm[key]; !ok {
			t.Errorf("colorFuncMap missing key %q", key)
		}
	}
}

// mustGet is a test helper that extracts and type-asserts a FuncMap entry.
func mustGet[T any](t *testing.T, fm map[string]any, key string) T {
	t.Helper()
	v, ok := fm[key]
	if !ok {
		t.Fatalf("colorFuncMap missing key %q", key)
	}
	fn, ok := v.(T)
	if !ok {
		t.Fatalf("colorFuncMap[%q] has unexpected type %T", key, v)
	}
	return fn
}

func TestColorFunc_NoColor(t *testing.T) {
	fm := colorFuncMap()
	bold := mustGet[func(string) string](t, fm, "bold")
	if got := bold("test"); got != "test" {
		t.Errorf("bold(\"test\") with NoColor = %q, want \"test\"", got)
	}
}

func TestSandboxDisabled_NilSandbox(t *testing.T) {
	fm := colorFuncMap()
	fn := mustGet[func(*BannerData) bool](t, fm, "sandboxDisabled")
	data := &BannerData{}
	if fn(data) {
		t.Error("sandboxDisabled should be false when Sandbox is nil")
	}
}

func TestSandboxDisabled_True(t *testing.T) {
	fm := colorFuncMap()
	fn := mustGet[func(*BannerData) bool](t, fm, "sandboxDisabled")
	data := &BannerData{Sandbox: &SandboxInfo{Disabled: true}}
	if !fn(data) {
		t.Error("sandboxDisabled should be true when Sandbox.Disabled is true")
	}
}

func TestSandboxPorts_NilSandbox(t *testing.T) {
	fm := colorFuncMap()
	fn := mustGet[func(*BannerData) string](t, fm, "sandboxPorts")
	if got := fn(&BannerData{}); got != "" {
		t.Errorf("sandboxPorts with nil sandbox = %q, want empty", got)
	}
}

func TestSandboxPorts_All(t *testing.T) {
	fm := colorFuncMap()
	fn := mustGet[func(*BannerData) string](t, fm, "sandboxPorts")
	data := &BannerData{Sandbox: &SandboxInfo{Ports: "all"}}
	if got := fn(data); got != "" {
		t.Errorf("sandboxPorts with 'all' = %q, want empty", got)
	}
}

func TestSandboxPorts_Specific(t *testing.T) {
	fm := colorFuncMap()
	fn := mustGet[func(*BannerData) string](t, fm, "sandboxPorts")
	data := &BannerData{Sandbox: &SandboxInfo{Ports: "443, 53"}}
	if got := fn(data); got != "443, 53" {
		t.Errorf("sandboxPorts = %q, want \"443, 53\"", got)
	}
}

func TestHasCapOrExtra_Caps(t *testing.T) {
	fm := colorFuncMap()
	fn := mustGet[func(*BannerData) bool](t, fm, "hasCapOrExtra")
	data := &BannerData{Capabilities: []CapabilityDisplay{{Name: "docker"}}}
	if !fn(data) {
		t.Error("hasCapOrExtra should be true with capabilities")
	}
}

func TestHasCapOrExtra_ExtraWritable(t *testing.T) {
	fm := colorFuncMap()
	fn := mustGet[func(*BannerData) bool](t, fm, "hasCapOrExtra")
	data := &BannerData{ExtraWritable: []string{"/some/path"}}
	if !fn(data) {
		t.Error("hasCapOrExtra should be true with ExtraWritable")
	}
}

func TestHasCapOrExtra_Empty(t *testing.T) {
	fm := colorFuncMap()
	fn := mustGet[func(*BannerData) bool](t, fm, "hasCapOrExtra")
	data := &BannerData{}
	if fn(data) {
		t.Error("hasCapOrExtra should be false with no caps or extra paths")
	}
}

func TestSlice(t *testing.T) {
	fm := colorFuncMap()
	fn := mustGet[func([]string, int) []string](t, fm, "slice")
	got := fn([]string{"a", "b", "c"}, 1)
	if len(got) != 2 || got[0] != "b" || got[1] != "c" {
		t.Errorf("slice([a,b,c], 1) = %v, want [b,c]", got)
	}
}

func TestJoin(t *testing.T) {
	fm := colorFuncMap()
	fn := mustGet[func([]string, string) string](t, fm, "join")
	got := fn([]string{"a", "b"}, ", ")
	if got != "a, b" {
		t.Errorf("join = %q, want \"a, b\"", got)
	}
}

func TestHasItems(t *testing.T) {
	fm := colorFuncMap()
	fn := mustGet[func([]string) bool](t, fm, "hasItems")
	if fn(nil) {
		t.Error("hasItems(nil) should be false")
	}
	if !fn([]string{"x"}) {
		t.Error("hasItems([x]) should be true")
	}
}

func TestNetworkLabel_NilSandbox(t *testing.T) {
	fm := colorFuncMap()
	fn := mustGet[func(*BannerData) string](t, fm, "networkLabel")
	got := fn(&BannerData{})
	if got != "outbound" {
		t.Errorf("networkLabel with nil sandbox = %q, want \"outbound\"", got)
	}
}

func TestAgentDisplay_DifferentPath(t *testing.T) {
	fm := colorFuncMap()
	fn := mustGet[func(*BannerData) string](t, fm, "agentDisplay")
	data := &BannerData{AgentName: "claude", AgentPath: "/usr/bin/claude"}
	got := fn(data)
	if !strings.Contains(got, "claude") || !strings.Contains(got, "/usr/bin/claude") {
		t.Errorf("agentDisplay = %q, expected both name and path", got)
	}
}
