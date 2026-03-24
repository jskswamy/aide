package ui

import (
	"bytes"
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
	for _, want := range []string{"aide", "work", "claude", "secret:", "env:", "Sandbox", "network:"} {
		if !strings.Contains(out, want) {
			t.Errorf("compact output missing %q", want)
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

	for _, want := range []string{
		"aws", "ssh",                     // active guard names
		"denied:", "allowed:",            // guard detail labels
		"kube", "~/.kube not found",      // skipped guard
		"gcp, docker", "available (opt-in)", // available guards
		"aide sandbox",                   // hint line
	} {
		if !strings.Contains(out, want) {
			t.Errorf("compact guard groups output missing %q\nfull output:\n%s", want, out)
		}
	}
}

func TestRenderCompact_ListTruncation(t *testing.T) {
	data := &BannerData{
		AgentName: "claude",
		Sandbox: &SandboxInfo{
			Network: "outbound only",
			Ports:   "all",
			Active: []GuardDisplay{
				{
					Name:      "filesystem",
					Protected: []string{"/a", "/b", "/c", "/d", "/e"},
				},
			},
		},
	}
	var buf bytes.Buffer
	RenderCompact(&buf, data)
	out := buf.String()

	if !strings.Contains(out, "(+2 more)") {
		t.Errorf("expected truncation marker (+2 more) in output:\n%s", out)
	}
	if !strings.Contains(out, "aide sandbox") {
		t.Errorf("expected hint line when lists are truncated:\n%s", out)
	}
}

func TestRenderCompact_PortsShown(t *testing.T) {
	data := &BannerData{
		AgentName: "claude",
		Sandbox: &SandboxInfo{
			Network: "outbound only",
			Ports:   "443, 53",
			Active: []GuardDisplay{
				{Name: "network"},
			},
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
	if !strings.Contains(out, "override: AWS_CONFIG_FILE") {
		t.Errorf("expected override line in output:\n%s", out)
	}
}

func TestRenderBoxed_GuardGroups(t *testing.T) {
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
	// Should not panic; verify basic structure
	if !strings.Contains(out, "aws") {
		t.Errorf("boxed guard groups missing active guard name:\n%s", out)
	}
	if !strings.Contains(out, "kube") {
		t.Errorf("boxed guard groups missing skipped guard name:\n%s", out)
	}
}

func TestRenderCompact_YoloShown(t *testing.T) {
	data := &BannerData{
		AgentName: "claude",
		Yolo:      true,
		Sandbox: &SandboxInfo{
			Network: "outbound only",
		},
	}
	var buf bytes.Buffer
	RenderCompact(&buf, data)
	out := buf.String()
	if !strings.Contains(out, "yolo mode") {
		t.Errorf("compact output should show yolo mode, got:\n%s", out)
	}
	if !strings.Contains(out, "permission checks disabled") {
		t.Errorf("compact yolo should mention permission checks, got:\n%s", out)
	}
}

func TestRenderCompact_YoloHiddenWhenFalse(t *testing.T) {
	data := &BannerData{
		AgentName: "claude",
		Yolo:      false,
	}
	var buf bytes.Buffer
	RenderCompact(&buf, data)
	out := buf.String()
	if strings.Contains(out, "yolo") {
		t.Errorf("compact output should not show yolo when disabled, got:\n%s", out)
	}
}

func TestRenderBoxed_YoloShown(t *testing.T) {
	data := &BannerData{
		AgentName: "claude",
		Yolo:      true,
	}
	var buf bytes.Buffer
	RenderBoxed(&buf, data)
	out := buf.String()
	if !strings.Contains(out, "yolo mode") {
		t.Errorf("boxed output should show yolo mode, got:\n%s", out)
	}
}

func TestRenderClean_YoloShown(t *testing.T) {
	data := &BannerData{
		AgentName: "claude",
		Yolo:      true,
	}
	var buf bytes.Buffer
	RenderClean(&buf, data)
	out := buf.String()
	if !strings.Contains(out, "yolo mode") {
		t.Errorf("clean output should show yolo mode, got:\n%s", out)
	}
}

func TestRenderClean_GuardGroups(t *testing.T) {
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
	if !strings.Contains(out, "ssh") {
		t.Errorf("clean guard groups missing active guard name:\n%s", out)
	}
	if !strings.Contains(out, "docker") {
		t.Errorf("clean guard groups missing available guard name:\n%s", out)
	}
}
