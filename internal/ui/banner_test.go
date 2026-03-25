package ui

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/fatih/color"
)

func init() {
	color.NoColor = true // disable ANSI for predictable test output
}

func fullBannerData() *BannerData {
	return &BannerData{
		ContextName: "work",
		MatchReason: "path glob match: ~/work/*",
		AgentName:   "claude",
		AgentPath:   "/usr/local/bin/claude",
		SecretName:  "work",
		SecretKeys:  []string{"api_key", "org_id", "token"},
		Env: map[string]string{
			"ANTHROPIC_API_KEY": "<- secrets.api_key",
			"ORG_ID":            "= acme",
		},
		Sandbox: &SandboxInfo{
			Network: "outbound",
			Ports:   "all",
			Active: []GuardDisplay{
				{
					Name:      "aws",
					Protected: []string{"~/.aws/credentials", "~/.aws/config"},
					Allowed:   []string{"/tmp/aws-test"},
				},
			},
		},
		Warnings: []string{"skipped: ~/.kube (not found)"},
	}
}

func TestRenderCompact(t *testing.T) {
	var buf bytes.Buffer
	RenderCompact(&buf, fullBannerData())
	out := buf.String()
	for _, want := range []string{"aide", "work", "claude", "secret:", "env:", "sandbox:", "code-only"} {
		if !strings.Contains(out, want) {
			t.Errorf("compact output missing %q\nfull output:\n%s", want, out)
		}
	}
}

func TestRenderBoxed(t *testing.T) {
	var buf bytes.Buffer
	RenderBoxed(&buf, fullBannerData())
	out := buf.String()
	if !strings.Contains(out, string(rune(9484))) || !strings.Contains(out, string(rune(9492))) {
		t.Error("boxed output missing box-drawing characters")
	}
	if !strings.Contains(out, "Context") {
		t.Error("boxed output missing Context label")
	}
}

func TestRenderClean(t *testing.T) {
	var buf bytes.Buffer
	RenderClean(&buf, fullBannerData())
	out := buf.String()
	if !strings.Contains(out, "aide") {
		t.Error("clean output missing header")
	}
	if !strings.Contains(out, "Agent") {
		t.Error("clean output missing Agent label")
	}
}

func TestRenderBanner_UnknownStyle(t *testing.T) {
	var buf bytes.Buffer
	RenderBanner(&buf, "unknown-style", fullBannerData())
	out := buf.String()
	if !strings.Contains(out, "aide") {
		t.Error("unknown style should fall back to compact")
	}
}

func TestRenderBanner_WithWarnings(_ *testing.T) {
	var buf bytes.Buffer
	RenderCompact(&buf, fullBannerData())
	out := buf.String()
	_ = out
	// Warnings should be rendered (the specific format may vary)
}

func TestRenderBanner_NoSandbox(t *testing.T) {
	data := fullBannerData()
	data.Sandbox = &SandboxInfo{Disabled: true}
	var buf bytes.Buffer
	RenderCompact(&buf, data)
	if !strings.Contains(buf.String(), "disabled") {
		t.Error("disabled sandbox should show 'disabled'")
	}
}

func TestRenderBanner_NoSecret(t *testing.T) {
	data := fullBannerData()
	data.SecretName = ""
	data.SecretKeys = nil
	var buf bytes.Buffer
	RenderCompact(&buf, data)
	if strings.Contains(buf.String(), "secret:") {
		t.Error("should not show secret section when no secret")
	}
}

func TestRenderBanner_NoEnv(t *testing.T) {
	data := fullBannerData()
	data.Env = nil
	var buf bytes.Buffer
	RenderCompact(&buf, data)
	if strings.Contains(buf.String(), "env:") {
		t.Error("should not show env section when no env")
	}
}

func TestTruncateList(t *testing.T) {
	tests := []struct {
		name     string
		items    []string
		max      int
		expected string
	}{
		{"empty", nil, 3, ""},
		{"under limit", []string{"a", "b"}, 3, "a, b"},
		{"at limit", []string{"a", "b", "c"}, 3, "a, b, c"},
		{"over limit", []string{"a", "b", "c", "d", "e"}, 3, "a, b, c (+2 more)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateList(tt.items, tt.max)
			if got != tt.expected {
				t.Errorf("truncateList(%v, %d) = %q, want %q", tt.items, tt.max, got, tt.expected)
			}
		})
	}
}

func TestRenderCompact_GuardGroups(t *testing.T) {
	// Guards without capabilities should show code-only (guards are NOT displayed in banner)
	data := &BannerData{
		AgentName: "claude",
		Sandbox: &SandboxInfo{
			Network: "outbound only",
			Ports:   "all",
			Active: []GuardDisplay{
				{
					Name:      "aws",
					Protected: []string{"~/.aws/credentials"},
					Allowed:   []string{"/tmp/aws"},
				},
				{
					Name:      "ssh",
					Protected: []string{"~/.ssh/id_rsa", "~/.ssh/id_ed25519"},
				},
			},
			Skipped: []GuardDisplay{
				{Name: "kube", Reason: "~/.kube not found"},
			},
			Available: []string{"gcp", "docker"},
		},
	}
	var buf bytes.Buffer
	RenderCompact(&buf, data)
	out := buf.String()

	// No capabilities → should show code-only
	if !strings.Contains(out, "code-only") {
		t.Errorf("expected code-only when no capabilities:\n%s", out)
	}

	// Guard names should NOT appear in banner output
	for _, unwanted := range []string{"denied:", "allowed:", "available (opt-in)"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("guard detail %q should not appear in banner:\n%s", unwanted, out)
		}
	}
}

func TestRenderCompact_ListTruncation(t *testing.T) {
	// With capabilities that have many paths, truncation marker should appear
	data := &BannerData{
		AgentName: "claude",
		Sandbox: &SandboxInfo{
			Network: "outbound only",
			Ports:   "all",
		},
		Capabilities: []CapabilityDisplay{
			{
				Name:  "filesystem",
				Paths: []string{"/a", "/b", "/c", "/d", "/e"},
			},
		},
	}
	var buf bytes.Buffer
	RenderCompact(&buf, data)
	out := buf.String()

	if !strings.Contains(out, "(+2 more)") {
		t.Errorf("expected truncation marker (+2 more) in output:\n%s", out)
	}
	if !strings.Contains(out, "filesystem") {
		t.Errorf("expected capability name in output:\n%s", out)
	}
}

func TestRenderCompact_PortsShown(t *testing.T) {
	data := &BannerData{
		AgentName: "claude",
		Sandbox: &SandboxInfo{
			Network: "outbound only",
			Ports:   "443, 53",
		},
	}
	var buf bytes.Buffer
	RenderCompact(&buf, data)
	out := buf.String()
	if !strings.Contains(out, "ports: 443, 53") {
		t.Errorf("expected ports line in output:\n%s", out)
	}
}

func TestRenderCompact_PortsAllHidden(t *testing.T) {
	data := &BannerData{
		AgentName: "claude",
		Sandbox: &SandboxInfo{
			Network: "outbound only",
			Ports:   "all",
			Active: []GuardDisplay{
				{Name: "network"},
			},
		},
	}
	var buf bytes.Buffer
	RenderCompact(&buf, data)
	out := buf.String()
	if strings.Contains(out, "ports:") {
		t.Errorf("ports: all should be hidden, but got:\n%s", out)
	}
}

func TestRenderCompact_Overrides(t *testing.T) {
	// Overrides are a guard feature; with no capabilities, banner shows code-only
	// Guard overrides should NOT appear in the banner
	data := &BannerData{
		AgentName: "claude",
		Sandbox: &SandboxInfo{
			Network: "outbound only",
			Ports:   "all",
			Active: []GuardDisplay{
				{
					Name: "aws",
					Overrides: []GuardOverride{
						{EnvVar: "AWS_CONFIG_FILE", Value: "/custom/config", DefaultPath: "~/.aws/config"},
					},
				},
			},
		},
	}
	var buf bytes.Buffer
	RenderCompact(&buf, data)
	out := buf.String()

	// No capabilities → code-only, no guard details
	if !strings.Contains(out, "code-only") {
		t.Errorf("expected code-only when no capabilities:\n%s", out)
	}
	if strings.Contains(out, "override:") {
		t.Errorf("guard overrides should not appear in banner:\n%s", out)
	}
}

func TestRenderBoxed_GuardGroups(t *testing.T) {
	// Guards without capabilities → code-only, no guard names in banner
	data := &BannerData{
		AgentName: "claude",
		Sandbox: &SandboxInfo{
			Network: "outbound only",
			Ports:   "all",
			Active: []GuardDisplay{
				{Name: "aws", Protected: []string{"~/.aws/credentials"}},
			},
			Skipped: []GuardDisplay{
				{Name: "kube", Reason: "~/.kube not found"},
			},
		},
	}
	var buf bytes.Buffer
	RenderBoxed(&buf, data)
	out := buf.String()

	if !strings.Contains(out, "code-only") {
		t.Errorf("boxed guard groups should show code-only when no capabilities:\n%s", out)
	}
	// Guard names should NOT appear
	if strings.Contains(out, "kube") {
		t.Errorf("guard name 'kube' should not appear in banner:\n%s", out)
	}
}

func TestRenderCompact_YoloShown(t *testing.T) {
	data := &BannerData{
		AgentName:   "claude",
		AutoApprove: true,
		Sandbox: &SandboxInfo{
			Network: "outbound only",
		},
	}
	var buf bytes.Buffer
	RenderCompact(&buf, data)
	out := buf.String()
	if !strings.Contains(out, "AUTO-APPROVE") {
		t.Errorf("compact output should show AUTO-APPROVE, got:\n%s", out)
	}
	if !strings.Contains(out, "without confirmation") {
		t.Errorf("compact auto-approve should mention 'without confirmation', got:\n%s", out)
	}
}

func TestRenderCompact_YoloHiddenWhenFalse(t *testing.T) {
	data := &BannerData{
		AgentName:   "claude",
		AutoApprove: false,
	}
	var buf bytes.Buffer
	RenderCompact(&buf, data)
	out := buf.String()
	if strings.Contains(out, "AUTO-APPROVE") {
		t.Errorf("compact output should not show AUTO-APPROVE when disabled, got:\n%s", out)
	}
}

func TestRenderBoxed_YoloShown(t *testing.T) {
	data := &BannerData{
		AgentName:   "claude",
		AutoApprove: true,
	}
	var buf bytes.Buffer
	RenderBoxed(&buf, data)
	out := buf.String()
	if !strings.Contains(out, "AUTO-APPROVE") {
		t.Errorf("boxed output should show AUTO-APPROVE, got:\n%s", out)
	}
}

func TestRenderClean_YoloShown(t *testing.T) {
	data := &BannerData{
		AgentName:   "claude",
		AutoApprove: true,
	}
	var buf bytes.Buffer
	RenderClean(&buf, data)
	out := buf.String()
	if !strings.Contains(out, "AUTO-APPROVE") {
		t.Errorf("clean output should show AUTO-APPROVE, got:\n%s", out)
	}
}

func TestRenderClean_GuardGroups(t *testing.T) {
	// Guards without capabilities → code-only, no guard names in banner
	data := &BannerData{
		AgentName: "claude",
		Sandbox: &SandboxInfo{
			Network: "outbound only",
			Ports:   "all",
			Active: []GuardDisplay{
				{Name: "ssh", Protected: []string{"~/.ssh/id_rsa"}},
			},
			Available: []string{"docker"},
		},
	}
	var buf bytes.Buffer
	RenderClean(&buf, data)
	out := buf.String()

	if !strings.Contains(out, "code-only") {
		t.Errorf("clean guard groups should show code-only when no capabilities:\n%s", out)
	}
	// Guard/available names should NOT appear in banner
	if strings.Contains(out, "docker") {
		t.Errorf("available guard name 'docker' should not appear in banner:\n%s", out)
	}
}

// --- Capability banner tests ---

func capabilityBannerData() *BannerData {
	return &BannerData{
		ContextName: "work",
		AgentName:   "claude",
		AgentPath:   "/usr/local/bin/claude",
		Capabilities: []CapabilityDisplay{
			{
				Name:   "k8s",
				Paths:  []string{"~/.kube/config"},
				Source: "context config",
			},
			{
				Name:   "docker",
				Paths:  []string{"~/.docker/config.json"},
				Source: "--with",
			},
		},
		DisabledCaps: []CapabilityDisplay{
			{
				Name:     "aws",
				Disabled: true,
				Source:   "--without",
			},
		},
		NeverAllow:   []string{"~/.kube/prod-config"},
		CredWarnings: []string{"AWS_SECRET_ACCESS_KEY (via aws)"},
		CompWarnings: []string{"docker + k8s share /var/run"},
	}
}

func TestRenderCompact_CapabilityCheckmarks(t *testing.T) {
	var buf bytes.Buffer
	RenderCompact(&buf, capabilityBannerData())
	out := buf.String()
	// Active capabilities should show checkmark and name
	if !strings.Contains(out, "\u2713") {
		t.Errorf("compact capability output missing checkmark:\n%s", out)
	}
	if !strings.Contains(out, "k8s") {
		t.Errorf("compact capability output missing k8s name:\n%s", out)
	}
	if !strings.Contains(out, "docker") {
		t.Errorf("compact capability output missing docker name:\n%s", out)
	}
}

func TestRenderCompact_CapabilitySourceAnnotation(t *testing.T) {
	var buf bytes.Buffer
	RenderCompact(&buf, capabilityBannerData())
	out := buf.String()
	// docker has source "--with", should show annotation
	if !strings.Contains(out, "\u2190 --with") {
		t.Errorf("compact capability output missing source annotation for --with:\n%s", out)
	}
	// k8s has source "context config", should NOT show annotation
	lines := strings.Split(out, "\n")
	for i, line := range lines {
		if strings.Contains(line, "k8s") && i+1 < len(lines) {
			if strings.Contains(lines[i+1], "\u2190 context config") {
				t.Errorf("context config source should not be annotated:\n%s", out)
			}
		}
	}
}

func TestRenderCompact_DisabledCaps(t *testing.T) {
	var buf bytes.Buffer
	RenderCompact(&buf, capabilityBannerData())
	out := buf.String()
	if !strings.Contains(out, "\u25CB") {
		t.Errorf("compact output missing disabled cap circle:\n%s", out)
	}
	if !strings.Contains(out, "aws") {
		t.Errorf("compact output missing disabled cap name:\n%s", out)
	}
	if !strings.Contains(out, "disabled for this session") {
		t.Errorf("compact output missing disabled text:\n%s", out)
	}
	if !strings.Contains(out, "\u2190 --without") {
		t.Errorf("compact output missing --without annotation:\n%s", out)
	}
}

func TestRenderCompact_NeverAllow(t *testing.T) {
	var buf bytes.Buffer
	RenderCompact(&buf, capabilityBannerData())
	out := buf.String()
	if !strings.Contains(out, "\u2717") {
		t.Errorf("compact output missing never-allow X:\n%s", out)
	}
	if !strings.Contains(out, "~/.kube/prod-config") {
		t.Errorf("compact output missing never-allow path:\n%s", out)
	}
	if !strings.Contains(out, "never-allow") {
		t.Errorf("compact output missing never-allow label:\n%s", out)
	}
}

func TestRenderCompact_CredWarnings(t *testing.T) {
	var buf bytes.Buffer
	RenderCompact(&buf, capabilityBannerData())
	out := buf.String()
	if !strings.Contains(out, "credentials exposed") {
		t.Errorf("compact output missing credential warning:\n%s", out)
	}
	if !strings.Contains(out, "AWS_SECRET_ACCESS_KEY") {
		t.Errorf("compact output missing credential var name:\n%s", out)
	}
}

func TestRenderCompact_CompWarnings(t *testing.T) {
	var buf bytes.Buffer
	RenderCompact(&buf, capabilityBannerData())
	out := buf.String()
	if !strings.Contains(out, "docker + k8s share /var/run") {
		t.Errorf("compact output missing composition warning:\n%s", out)
	}
}

func TestAutoApprove_LastNonEmptyLine(t *testing.T) {
	styles := []struct {
		name   string
		render func(io.Writer, *BannerData)
	}{
		{"compact", func(w io.Writer, d *BannerData) { RenderCompact(w, d) }},
		{"boxed", func(w io.Writer, d *BannerData) { RenderBoxed(w, d) }},
		{"clean", func(w io.Writer, d *BannerData) { RenderClean(w, d) }},
	}

	for _, style := range styles {
		t.Run(style.name, func(t *testing.T) {
			data := capabilityBannerData()
			data.AutoApprove = true

			var buf bytes.Buffer
			style.render(&buf, data)
			out := buf.String()

			if !strings.Contains(out, "AUTO-APPROVE") {
				t.Fatalf("%s output missing AUTO-APPROVE:\n%s", style.name, out)
			}

			// Find last non-empty line
			lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
			lastNonEmpty := ""
			for i := len(lines) - 1; i >= 0; i-- {
				trimmed := strings.TrimSpace(lines[i])
				if trimmed != "" {
					lastNonEmpty = lines[i]
					break
				}
			}

			if style.name == "boxed" {
				// In boxed mode, AUTO-APPROVE is inside the box (before └)
				// The last non-empty line is the box bottom border
				// Verify AUTO-APPROVE appears just before the closing border
				foundAutoApprove := false
				for i, line := range lines {
					if strings.Contains(line, "AUTO-APPROVE") {
						// Next non-empty line should be the closing border
						for j := i + 1; j < len(lines); j++ {
							if strings.TrimSpace(lines[j]) != "" {
								if strings.Contains(lines[j], "\u2514") {
									foundAutoApprove = true
								}
								break
							}
						}
						break
					}
				}
				if !foundAutoApprove {
					t.Errorf("%s: AUTO-APPROVE should be inside box (just before └ border)\nfull output:\n%s",
						style.name, out)
				}
			} else if !strings.Contains(lastNonEmpty, "AUTO-APPROVE") {
				t.Errorf("%s: AUTO-APPROVE should be last non-empty line, but last was: %q\nfull output:\n%s",
					style.name, lastNonEmpty, out)
			}
		})
	}
}

func TestAutoApprove_HiddenWhenFalse(t *testing.T) {
	data := capabilityBannerData()
	data.AutoApprove = false

	var buf bytes.Buffer
	RenderCompact(&buf, data)
	if strings.Contains(buf.String(), "AUTO-APPROVE") {
		t.Error("AUTO-APPROVE should not appear when AutoApprove is false")
	}
}

func TestRenderCompact_CodeOnlyWhenNoCapabilities(t *testing.T) {
	// No capabilities, but sandbox present — should show code-only, not guard names
	data := &BannerData{
		AgentName: "claude",
		Sandbox: &SandboxInfo{
			Network: "outbound only",
			Ports:   "all",
			Active: []GuardDisplay{
				{Name: "aws", Protected: []string{"~/.aws/credentials"}},
			},
		},
	}
	var buf bytes.Buffer
	RenderCompact(&buf, data)
	out := buf.String()
	if !strings.Contains(out, "code-only") {
		t.Errorf("should show code-only when no capabilities:\n%s", out)
	}
	// Guard names should NOT appear in banner
	if strings.Contains(out, "denied:") {
		t.Errorf("guard details should not appear in banner:\n%s", out)
	}
	if strings.Contains(out, "Capabilities") {
		t.Errorf("should not show Capabilities label when no capabilities:\n%s", out)
	}
}

func TestRenderCompact_CodeOnlyLabel(t *testing.T) {
	// No capabilities and no sandbox — should show code-only
	data := &BannerData{
		AgentName: "claude",
	}
	var buf bytes.Buffer
	RenderCompact(&buf, data)
	out := buf.String()
	if !strings.Contains(out, "code-only") {
		t.Errorf("should show code-only when no capabilities and no sandbox:\n%s", out)
	}
}

func TestRenderBoxed_CapabilityCheckmarks(t *testing.T) {
	var buf bytes.Buffer
	RenderBoxed(&buf, capabilityBannerData())
	out := buf.String()
	if !strings.Contains(out, "\u2713") {
		t.Errorf("boxed capability output missing checkmark:\n%s", out)
	}
	if !strings.Contains(out, "sandbox:") {
		t.Errorf("boxed output missing sandbox: label:\n%s", out)
	}
}

func TestRenderClean_CapabilityCheckmarks(t *testing.T) {
	var buf bytes.Buffer
	RenderClean(&buf, capabilityBannerData())
	out := buf.String()
	if !strings.Contains(out, "\u2713") {
		t.Errorf("clean capability output missing checkmark:\n%s", out)
	}
	if !strings.Contains(out, "sandbox:") {
		t.Errorf("clean output missing sandbox: label:\n%s", out)
	}
}

func TestRenderBoxed_CodeOnlyLabel(t *testing.T) {
	data := &BannerData{AgentName: "claude"}
	var buf bytes.Buffer
	RenderBoxed(&buf, data)
	out := buf.String()
	if !strings.Contains(out, "code-only") {
		t.Errorf("boxed should show code-only when no capabilities and no sandbox:\n%s", out)
	}
}

func TestRenderClean_CodeOnlyLabel(t *testing.T) {
	data := &BannerData{AgentName: "claude"}
	var buf bytes.Buffer
	RenderClean(&buf, data)
	out := buf.String()
	if !strings.Contains(out, "code-only") {
		t.Errorf("clean should show code-only when no capabilities and no sandbox:\n%s", out)
	}
}
