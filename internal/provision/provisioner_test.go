package provision_test

import (
	"testing"

	"github.com/jskswamy/aide/internal/provision"
)

func TestPluginAndMCPServerZeroValue(_ *testing.T) {
	var p provision.Plugin
	var m provision.MCPServer
	_ = p
	_ = m
}

func TestSourceShapeStrings(t *testing.T) {
	cases := map[provision.SourceShape]string{
		provision.ShapeMarketplace: "marketplace",
		provision.ShapeURLDirect:   "url-direct",
	}
	for s, want := range cases {
		if got := s.String(); got != want {
			t.Errorf("SourceShape(%d).String() = %q, want %q", s, got, want)
		}
	}
}

func TestMarketplaceZeroValue(t *testing.T) {
	var m provision.Marketplace
	if m.Key != "" || m.Source != "" || m.Name != "" {
		t.Errorf("zero Marketplace = %+v", m)
	}
}

func TestOpKindStrings(t *testing.T) {
	cases := map[provision.OpKind]string{
		provision.OpInstall:   "install",
		provision.OpUpdate:    "update",
		provision.OpUninstall: "uninstall",
		provision.OpAdopt:     "adopt",
		provision.OpIgnore:    "ignore",
	}
	for k, want := range cases {
		if got := k.String(); got != want {
			t.Errorf("OpKind(%d).String() = %q, want %q", k, got, want)
		}
	}
}

func TestKindHookString(t *testing.T) {
	if provision.KindHook.String() != "hook" {
		t.Errorf("KindHook.String() = %q, want %q", provision.KindHook.String(), "hook")
	}
}
