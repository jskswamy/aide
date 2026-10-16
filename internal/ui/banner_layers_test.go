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

// The approve hint names the file even for a local one, and the
// wants line covers loosening settings beyond capabilities and paths.
func TestTrustFileLines_NameFileAndLooseningWants(t *testing.T) {
	ti := &TrustInfo{Status: "untrusted", Source: "local", Path: "~/src/wt/.aide.yaml", Wants: TrustWants{
		Readable: []string{"/etc"}, Network: "unrestricted", Guards: []string{"base"},
		Allow: []string{"(allow mach-lookup)"}, SandboxDisabled: true, AutoApprove: true,
	}}
	if got := trustFileStatusLine(ti); !strings.Contains(got, "aide trust ~/src/wt/.aide.yaml") {
		t.Errorf("status line = %q", got)
	}
	got := trustFileWantsLine(ti)
	for _, want := range []string{"/etc", "network unrestricted", "guards base", "(allow mach-lookup)", "sandbox off", "auto-approve"} {
		if !strings.Contains(got, want) {
			t.Errorf("wants line %q missing %q", got, want)
		}
	}
}
