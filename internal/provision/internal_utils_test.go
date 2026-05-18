package provision

import (
	"reflect"
	"sort"
	"testing"
)

func TestExtractVersion(t *testing.T) {
	cases := map[string]string{
		"":               "",
		"plugin":         "",
		"plugin@1.2.3":   "1.2.3",
		"plugin@@1":      "1",
		"plugin@marketX": "marketX",
	}
	for in, want := range cases {
		if got := extractVersion(in); got != want {
			t.Errorf("extractVersion(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCopyMCPMap(t *testing.T) {
	src := map[string]MCPServer{
		"a": {Command: "ac", Args: []string{"x"}},
		"b": {URL: "http://b"},
	}
	dst := copyMCPMap(src)
	if !reflect.DeepEqual(src, dst) {
		t.Errorf("copy not equal: src=%+v dst=%+v", src, dst)
	}
	// mutation of dst must not bleed back
	delete(dst, "a")
	if _, ok := src["a"]; !ok {
		t.Error("mutating copy leaked into source")
	}
}

func TestMCPEqual(t *testing.T) {
	base := MCPServer{Command: "c", Args: []string{"x"}, Env: map[string]string{"K": "v"}}
	cases := []struct {
		name string
		a, b MCPServer
		want bool
	}{
		{"identical", base, base, true},
		{"diff command", base, MCPServer{Command: "z", Args: base.Args, Env: base.Env}, false},
		{"diff url", MCPServer{URL: "http://a"}, MCPServer{URL: "http://b"}, false},
		{"diff args", base, MCPServer{Command: "c", Args: []string{"y"}, Env: base.Env}, false},
		{"diff env value", base, MCPServer{Command: "c", Args: base.Args, Env: map[string]string{"K": "x"}}, false},
		{"diff env length", base, MCPServer{Command: "c", Args: base.Args, Env: map[string]string{}}, false},
		{"both empty", MCPServer{}, MCPServer{}, true},
	}
	for _, tc := range cases {
		if got := mcpEqual(tc.a, tc.b); got != tc.want {
			t.Errorf("%s: mcpEqual = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestMergeEnv(t *testing.T) {
	// empty override returns parent unchanged
	parent := []string{"A=1", "B=2"}
	if got := mergeEnv(parent, nil); !reflect.DeepEqual(got, parent) {
		t.Errorf("nil override should return parent, got %v", got)
	}

	// override appends new entries
	got := mergeEnv(parent, map[string]string{"C": "3", "D": "4"})
	sort.Strings(got)
	want := []string{"A=1", "B=2", "C=3", "D=4"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("mergeEnv = %v, want %v", got, want)
	}
}
