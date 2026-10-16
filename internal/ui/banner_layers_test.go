package ui

import (
	"strings"
	"testing"
)

// Each .aide.yaml that is not trusted gets its own trust block, and
// grants inherited from a main checkout are listed with their source.
func TestRenderBanner_ShowsEachTrustFileAndInheritedGrants(t *testing.T) {
	data := &BannerData{
		ContextName: "work",
		AgentName:   "claude",
		TrustFiles: []TrustInfo{
			{Status: "untrusted", Source: "inherited", Path: "~/src/main/.aide.yaml", Wants: TrustWants{Capabilities: []string{"aws"}}},
			{Status: "denied", Source: "local", Path: "~/src/wt/.aide.yaml"},
		},
		InheritedGrants: []InheritedGrant{
			{Kind: "writable", Value: "/src/main/build", Source: "~/src/main/.aide.yaml"},
			{Kind: "capability", Value: "docker", Source: "~/src/main/.aide.yaml"},
		},
	}
	data.Trust = &data.TrustFiles[0]
	for _, style := range []string{"compact", "boxed", "clean"} {
		var buf strings.Builder
		if err := RenderBanner(&buf, style, data); err != nil {
			t.Fatalf("%s: %v", style, err)
		}
		out := buf.String()
		for _, want := range []string{
			"~/src/main/.aide.yaml", "inherited from main checkout", "aide trust ~/src/main/.aide.yaml",
			"aws capability",
			"denied at ~/src/wt/.aide.yaml",
			"writable", "/src/main/build", "docker",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("%s banner missing %q:\n%s", style, want, out)
			}
		}
		if n := strings.Count(out, "🚨"); n != 1 {
			t.Errorf("%s banner: %d untrusted blocks, want 1:\n%s", style, n, out)
		}
	}
}
