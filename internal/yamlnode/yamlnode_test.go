package yamlnode_test

import (
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/jskswamy/aide/internal/yamlnode"
)

func TestValue(t *testing.T) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte("a: 1\nb: {c: x}\n"), &doc); err != nil {
		t.Fatal(err)
	}
	if v := yamlnode.Value(&doc, "a"); v == nil || v.Value != "1" {
		t.Errorf("Value(doc, a) = %v, want scalar 1 (documents are unwrapped)", v)
	}
	if v := yamlnode.Value(yamlnode.Value(&doc, "b"), "c"); v == nil || v.Value != "x" {
		t.Errorf("Value(b, c) = %v, want x", v)
	}
	if v := yamlnode.Value(&doc, "missing"); v != nil {
		t.Errorf("Value(doc, missing) = %v, want nil", v)
	}
	if v := yamlnode.Value(nil, "a"); v != nil {
		t.Errorf("Value(nil, a) = %v, want nil", v)
	}
}

func TestStr(t *testing.T) {
	n := yamlnode.Str("1")
	if n.Kind != yaml.ScalarNode || n.Tag != "!!str" || n.Value != "1" {
		t.Errorf("Str(1) = %+v, want a !!str scalar", n)
	}
}

func TestMarshal_TwoSpaceIndent(t *testing.T) {
	got, err := yamlnode.Marshal(map[string]any{
		"outer": map[string]string{"inner": "val"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "outer:\n  inner: val\n"
	if string(got) != want {
		t.Errorf("Marshal() = %q, want %q", string(got), want)
	}
}

func TestMerge_PreservesCommentsDocHeaderAndKeyOrder(t *testing.T) {
	existing := []byte("---\n# profile header\nagent: claude # inline comment\nconfig_dir_name: work\nstale: gone\n")
	updated := []byte("agent: claude\nenv:\n  EDITOR: nvim\nconfig_dir_name: work\n")
	got := string(yamlnode.Merge(existing, updated))
	want := "---\n# profile header\nagent: claude # inline comment\nconfig_dir_name: work\nenv:\n  EDITOR: nvim\n"
	if got != want {
		t.Errorf("Merge() =\n%s\nwant:\n%s", got, want)
	}
}
